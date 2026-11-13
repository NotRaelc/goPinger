package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"goPinger/query"
)

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

// resolveIconURL выбирает картинку на сервере: для Minecraft она лежит в payload
// (base64), для Source — по appid в локальной папке, иначе пусто (фронт покажет
// дефолтную текстуру).
func resolveIconURL(p *query.Server) string {
	if p == nil {
		return ""
	}
	if p.Icon != "" {
		return p.Icon
	}
	if p.GameKind == "source" && p.GameID > 0 {
		path := fmt.Sprintf("frontend/static/images/games/%d.png", p.GameID)
		if _, err := os.Stat(path); err == nil {
			return fmt.Sprintf("/static/images/games/%d.png", p.GameID)
		}
	}
	return ""
}

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
	return pool.QueryRow(ctx, `
		INSERT INTO servers(user_id, address, type, interval_sec, label, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, s.UserID, s.Address, s.Type, s.IntervalSec, s.Label, s.Enabled).
		Scan(&s.ID, new(time.Time), new(time.Time))
}

func updateServer(ctx context.Context, s Server) error {
	_, err := pool.Exec(ctx, `
		UPDATE servers
		SET address = $1, type = $2, interval_sec = $3, label = $4, enabled = $5, updated_at = NOW()
		WHERE id = $6 AND user_id = $7
	`, s.Address, s.Type, s.IntervalSec, s.Label, s.Enabled, s.ID, s.UserID)
	return err
}

func deleteServer(ctx context.Context, userID string, id int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM servers WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}
