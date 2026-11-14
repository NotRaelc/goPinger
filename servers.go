package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"goPinger/query"
)

// ---------- кеш списка серверов для поллера ----------

var (
	serversCacheMu  sync.RWMutex
	serversCache    []Server
	serversCacheExp time.Time
)

func invalidateServersCache() {
	serversCacheMu.Lock()
	serversCacheExp = time.Time{}
	serversCacheMu.Unlock()
}

func enabledServersCached(ctx context.Context) ([]Server, error) {
	serversCacheMu.RLock()
	if time.Now().Before(serversCacheExp) {
		out := serversCache
		serversCacheMu.RUnlock()
		return out, nil
	}
	serversCacheMu.RUnlock()

	list, err := listEnabledServers(ctx)
	if err != nil {
		return nil, err
	}
	serversCacheMu.Lock()
	serversCache = list
	serversCacheExp = time.Now().Add(30 * time.Second)
	serversCacheMu.Unlock()
	return list, nil
}

// ---------- иконки Source-серверов ----------

var (
	iconFilesMu sync.RWMutex
	iconFiles   = map[int64]bool{}
)

func loadIconFiles() {
	entries, err := os.ReadDir("frontend/static/images/games")
	if err != nil {
		return
	}
	iconFilesMu.Lock()
	defer iconFilesMu.Unlock()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if id, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".png"), 10, 64); err == nil {
			iconFiles[id] = true
		}
	}
}

func iconExists(id int64) bool {
	iconFilesMu.RLock()
	has, known := iconFiles[id]
	iconFilesMu.RUnlock()
	if known {
		return has
	}
	_, err := os.Stat(fmt.Sprintf("frontend/static/images/games/%d.png", id))
	exists := err == nil
	iconFilesMu.Lock()
	iconFiles[id] = exists
	iconFilesMu.Unlock()
	return exists
}

func resolveIconURL(p *query.Server) string {
	if p == nil {
		return ""
	}
	if p.Icon != "" {
		return p.Icon
	}
	if p.GameKind == "source" && p.GameID > 0 && iconExists(p.GameID) {
		return fmt.Sprintf("/static/images/games/%d.png", p.GameID)
	}
	return ""
}

func buildDTO(s Server, st *ServerState) ServerDTO {
	detected := query.ServerAuto
	if st.Payload != nil {
		detected = gameKindToType(st.Payload.GameKind)
	}
	interval := effectiveInterval(s, detected)
	var nextAt int64
	if s.Enabled {
		nextAt = time.Now().Add(interval).UnixMilli()
	}
	return ServerDTO{
		ID:                   s.ID,
		Address:              s.Address,
		Type:                 s.Type,
		IntervalSec:          s.IntervalSec,
		EffectiveIntervalSec: int(interval.Seconds()),
		NextPollAt:           nextAt,
		Label:                s.Label,
		Enabled:              s.Enabled,
		IconURL:              resolveIconURL(st.Payload),
		State:                st,
	}
}

// ---------- singleflight-кеш для /api/servers ----------

var listSF singleflight.Group

func listServersCached(ctx context.Context, userID string) ([]ServerDTO, error) {
	key := keyList(userID)
	if b, err := redisClient.Get(ctx, key).Bytes(); err == nil {
		var out []ServerDTO
		if json.Unmarshal(b, &out) == nil {
			return out, nil
		}
	}
	v, err, _ := listSF.Do(userID, func() (any, error) {
		out, err := listServers(ctx, userID)
		if err != nil {
			return nil, err
		}
		if b, e := json.Marshal(out); e == nil {
			redisClient.Set(ctx, key, b, 3*time.Second)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]ServerDTO), nil
}

// ---------- CRUD ----------

func listServers(ctx context.Context, userID string) ([]ServerDTO, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.id, s.address, s.type, s.interval_sec, s.label, s.enabled,
		       st.ok, st.error, st.payload, st.checked_at
		FROM servers s
		LEFT JOIN server_states st ON st.server_id = s.id
		WHERE s.user_id = $1
		ORDER BY s.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ServerDTO
	for rows.Next() {
		var (
			dto       ServerDTO
			ok        *bool
			errStr    *string
			payload   []byte
			checkedAt *time.Time
		)
		if err := rows.Scan(&dto.ID, &dto.Address, &dto.Type, &dto.IntervalSec,
			&dto.Label, &dto.Enabled, &ok, &errStr, &payload, &checkedAt); err != nil {
			return nil, err
		}
		var detected query.ServerType = query.ServerAuto
		if ok != nil && checkedAt != nil {
			dto.State = &ServerState{OK: *ok, CheckedAt: checkedAt.UnixMilli()}
			if errStr != nil {
				dto.State.Error = *errStr
			}
			if len(payload) > 0 {
				var srv queryServer
				if uerr := srv.Unmarshal(payload); uerr == nil {
					dto.State.Payload = srv.Ptr()
					dto.IconURL = resolveIconURL(dto.State.Payload)
					if dto.State.Payload != nil {
						detected = gameKindToType(dto.State.Payload.GameKind)
					}
				}
			}
		}
		dto.EffectiveIntervalSec = int(effectiveInterval(
			Server{Type: dto.Type, IntervalSec: dto.IntervalSec}, detected,
		).Seconds())
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Точный next_poll_at = now + TTL(keyNext) из Redis (совпадает с поллером).
	if len(out) > 0 {
		pipe := redisClient.Pipeline()
		cmds := make([]*redis.DurationCmd, len(out))
		for i := range out {
			cmds[i] = pipe.TTL(ctx, keyNext(out[i].ID))
		}
		_, _ = pipe.Exec(ctx)
		now := time.Now().UnixMilli()
		for i := range out {
			if !out[i].Enabled {
				continue
			}
			if ttl, err := cmds[i].Result(); err == nil && ttl > 0 {
				out[i].NextPollAt = now + ttl.Milliseconds()
			}
		}
	}
	return out, nil
}

func listEnabledServers(ctx context.Context) ([]Server, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, address, type, interval_sec, label, enabled
		FROM servers WHERE enabled = TRUE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Server
	for rows.Next() {
		var s Server
		if err := rows.Scan(&s.ID, &s.UserID, &s.Address, &s.Type,
			&s.IntervalSec, &s.Label, &s.Enabled); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func getServerByID(ctx context.Context, id int64) (Server, error) {
	var s Server
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, address, type, interval_sec, label, enabled
		FROM servers WHERE id = $1
	`, id).Scan(&s.ID, &s.UserID, &s.Address, &s.Type,
		&s.IntervalSec, &s.Label, &s.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func insertServer(ctx context.Context, s *Server) error {
	err := pool.QueryRow(ctx, `
		INSERT INTO servers(user_id, address, type, interval_sec, label, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, s.UserID, s.Address, s.Type, s.IntervalSec, s.Label, s.Enabled).Scan(&s.ID)
	if err == nil {
		invalidateServersCache()
		invalidateListCache(ctx, s.UserID)
	}
	return err
}

func updateServer(ctx context.Context, s Server) error {
	_, err := pool.Exec(ctx, `
		UPDATE servers
		SET address=$1, type=$2, interval_sec=$3, label=$4, enabled=$5, updated_at=NOW()
		WHERE id=$6 AND user_id=$7
	`, s.Address, s.Type, s.IntervalSec, s.Label, s.Enabled, s.ID, s.UserID)
	if err == nil {
		invalidateServersCache()
		invalidateListCache(ctx, s.UserID)
	}
	return err
}

func deleteServer(ctx context.Context, userID string, id int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM servers WHERE id=$1 AND user_id=$2`, id, userID)
	if err == nil {
		invalidateServersCache()
		invalidateListCache(ctx, userID)
	}
	return err
}
