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

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"goPinger/query"
)

var (
	pool     *pgxpool.Pool
	validate *validator.Validate
)

type RegisterUser struct {
	Username        string `json:"username" validate:"required,min=1,max=32,alphanum"`
	Password        string `json:"password" validate:"required,min=1,max=128,printascii"`
	PasswordConfirm string `json:"password_confirm" validate:"required,eqfield=Password"`
}

type LoginUser struct {
	Username string `json:"username" validate:"required,min=1,max=32,alphanum"`
	Password string `json:"password" validate:"required,min=1,max=128,printascii"`
}

type User struct {
	Username string `json:"username" validate:"required,min=1,max=32,alphanum"`
	Hash     string `json:"password" validate:"required,min=1"`
	Token    string `json:"token" validate:"required,min=1"`
}

type Server struct {
	ID          int64
	UserID      string
	Address     string
	Type        int
	IntervalSec int
	Label       string
	Enabled     bool
}

type ServerDTO struct {
	ID                   int64        `json:"id"`
	Address              string       `json:"address"`
	Type                 int          `json:"type"`
	IntervalSec          int          `json:"interval_sec"`
	EffectiveIntervalSec int          `json:"effective_interval_sec"`
	Label                string       `json:"label"`
	Enabled              bool         `json:"enabled"`
	IconURL              string       `json:"icon_url,omitempty"`
	State                *ServerState `json:"state,omitempty"`
}

// --- кеш списка серверов для поллера ---

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

// --- иконки Source-серверов ---

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
		name := strings.TrimSuffix(e.Name(), ".png")
		if id, err := strconv.ParseInt(name, 10, 64); err == nil {
			iconFiles[id] = true
		}
	}
}

func iconExists(gameID int64) bool {
	iconFilesMu.RLock()
	has, known := iconFiles[gameID]
	iconFilesMu.RUnlock()
	if known {
		return has
	}
	path := fmt.Sprintf("frontend/static/images/games/%d.png", gameID)
	_, err := os.Stat(path)
	exists := err == nil
	iconFilesMu.Lock()
	iconFiles[gameID] = exists
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
	return ServerDTO{
		ID:                   s.ID,
		Address:              s.Address,
		Type:                 s.Type,
		IntervalSec:          s.IntervalSec,
		EffectiveIntervalSec: int(intervalFor(queryTypeFromInt(s.Type)).Seconds()),
		Label:                s.Label,
		Enabled:              s.Enabled,
		IconURL:              resolveIconURL(st.Payload),
		State:                st,
	}
}

// --- singleflight-кеш для /api/servers ---

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

// --- CRUD ---

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
		dto.EffectiveIntervalSec = int(intervalFor(queryTypeFromInt(dto.Type)).Seconds())
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
				}
			}
		}
		out = append(out, dto)
	}
	return out, rows.Err()
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
		if err := rows.Scan(&s.ID, &s.UserID, &s.Address, &s.Type, &s.IntervalSec, &s.Label, &s.Enabled); err != nil {
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
	`, id).Scan(&s.ID, &s.UserID, &s.Address, &s.Type, &s.IntervalSec, &s.Label, &s.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func insertServer(ctx context.Context, s *Server) error {
	err := pool.QueryRow(ctx, `
		INSERT INTO servers(user_id, address, type, interval_sec, label, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, s.UserID, s.Address, s.Type, s.IntervalSec, s.Label, s.Enabled).
		Scan(&s.ID, new(time.Time), new(time.Time))
	if err == nil {
		invalidateServersCache()
		invalidateListCache(ctx, s.UserID)
	}
	return err
}

func updateServer(ctx context.Context, s Server) error {
	_, err := pool.Exec(ctx, `
		UPDATE servers
		SET address = $1, type = $2, interval_sec = $3, label = $4, enabled = $5, updated_at = NOW()
		WHERE id = $6 AND user_id = $7
	`, s.Address, s.Type, s.IntervalSec, s.Label, s.Enabled, s.ID, s.UserID)
	if err == nil {
		invalidateServersCache()
		invalidateListCache(ctx, s.UserID)
	}
	return err
}

func deleteServer(ctx context.Context, userID string, id int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM servers WHERE id = $1 AND user_id = $2`, id, userID)
	if err == nil {
		invalidateServersCache()
		invalidateListCache(ctx, userID)
	}
	return err
}
