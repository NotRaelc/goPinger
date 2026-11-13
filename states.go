package main

import (
	"context"
	"encoding/json"
	"time"

	"goPinger/query"
)

type ServerState struct {
	OK        bool          `json:"ok"`
	Error     string        `json:"error,omitempty"`
	CheckedAt int64         `json:"checked_at"`
	FromCache bool          `json:"from_cache,omitempty"`
	Payload   *query.Server `json:"payload,omitempty"`
}

// Обёртка для (un)marshal JSONB: позволяет не тянуть query.Server в другие файлы.
type queryServer query.Server

func (q *queryServer) Unmarshal(b []byte) error { return json.Unmarshal(b, (*query.Server)(q)) }
func (q *queryServer) Ptr() *query.Server       { return (*query.Server)(q) }

func stateOK(p query.Server) ServerState {
	return ServerState{OK: true, CheckedAt: time.Now().UnixMilli(), Payload: &p}
}

func stateErr(err error) ServerState {
	return ServerState{OK: false, CheckedAt: time.Now().UnixMilli(), Error: err.Error()}
}

func saveState(ctx context.Context, serverID int64, st ServerState) error {
	var payload any
	if st.Payload != nil {
		b, err := json.Marshal(st.Payload)
		if err == nil {
			payload = b
		}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO server_states(server_id, ok, error, payload, checked_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (server_id) DO UPDATE
		SET ok = $2, error = $3, payload = $4, checked_at = $5
	`, serverID, st.OK, nullableString(st.Error), payload, time.UnixMilli(st.CheckedAt))
	return err
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
