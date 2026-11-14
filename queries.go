package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"goPinger/query"
)

// effectiveInterval — единый источник интервала для сервера.
// Приоритет: s.IntervalSec > intervalFor(userType or detected).
func effectiveInterval(s Server, detected query.ServerType) time.Duration {
	userType := queryTypeFromInt(s.Type)
	var base time.Duration
	if userType == query.ServerAuto && detected != query.ServerAuto {
		base = intervalFor(detected)
	} else {
		base = intervalFor(userType)
	}
	if s.IntervalSec > 0 {
		custom := time.Duration(s.IntervalSec) * time.Second
		if custom < cfg.MinRealInterval {
			custom = cfg.MinRealInterval
		}
		return custom
	}
	return base
}

func stateOK(p query.Server) ServerState {
	return ServerState{OK: true, CheckedAt: time.Now().UnixMilli(), Payload: &p}
}

func stateErr(err error) ServerState {
	return ServerState{OK: false, CheckedAt: time.Now().UnixMilli(), Error: err.Error()}
}

func saveState(ctx context.Context, serverID int64, st ServerState) error {
	var payload any
	if st.Payload != nil {
		if b, err := json.Marshal(st.Payload); err == nil {
			payload = b
		}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO server_states(server_id, ok, error, payload, checked_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (server_id) DO UPDATE
		SET ok=$2, error=$3, payload=$4, checked_at=$5
	`, serverID, st.OK, nullable(st.Error), payload, time.UnixMilli(st.CheckedAt))
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func queryAddressWithType(ctx context.Context, address string, explicit query.ServerType) (ServerState, query.ServerType) {
	qctx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	defer cancel()

	if explicit != query.ServerAuto {
		if resp := query.QueryServerOpts(qctx, address, query.Options{Type: explicit}); resp.Error == nil {
			cacheType(ctx, address, explicit)
			return stateOK(resp.Payload), explicit
		}
	}
	if t, ok := getCachedType(ctx, address); ok && t != explicit {
		if resp := query.QueryServerOpts(qctx, address, query.Options{Type: t}); resp.Error == nil {
			return stateOK(resp.Payload), t
		}
	}
	resp := query.QueryServerOpts(qctx, address, query.Options{Type: query.ServerAuto})
	if resp.Error != nil {
		return stateErr(resp.Error), query.ServerAuto
	}
	t := gameKindToType(resp.Payload.GameKind)
	if t != query.ServerAuto {
		cacheType(ctx, address, t)
	}
	return stateOK(resp.Payload), t
}

// fetchState — единственный источник реальных запросов.
//   - кеш моложе intervalSec → возвращаем его (FromCache=true)
//   - иначе — берём lock и делаем реальный запрос; проигравший гонку
//     получает кеш победителя.
//
// Cache TTL = effectiveInterval от того, кто реально опросил.
func fetchState(ctx context.Context, address string, explicit query.ServerType, intervalSec int) (ServerState, query.ServerType, bool) {
	if st, _, ok := getStateCache(ctx, address); ok {
		detected := query.ServerAuto
		if st.Payload != nil {
			detected = gameKindToType(st.Payload.GameKind)
		}
		ourInterval := effectiveInterval(Server{Type: int(explicit), IntervalSec: intervalSec}, detected)
		age := time.Since(time.UnixMilli(st.CheckedAt))
		if age < ourInterval {
			st.FromCache = true
			return *st, detected, true
		}
	}

	release, ok := acquireServerLock(ctx, address)
	if !ok {
		if st, ok := waitForCache(ctx, address); ok {
			detected := query.ServerAuto
			if st.Payload != nil {
				detected = gameKindToType(st.Payload.GameKind)
			}
			st.FromCache = true
			return *st, detected, true
		}
		return ServerState{
			OK:        false,
			Error:     "server is busy, try again",
			CheckedAt: time.Now().UnixMilli(),
		}, query.ServerAuto, false
	}
	defer release()

	state, detected := queryAddressWithType(ctx, address, explicit)
	ttl := effectiveInterval(Server{Type: int(explicit), IntervalSec: intervalSec}, detected)
	saveStateCache(ctx, address, &state, ttl)
	slog.Debug("real query", "address", address, "type", detected.String(), "ok", state.OK)
	return state, detected, false
}

func bootstrapServerState(ctx context.Context, s Server) (ServerState, bool) {
	st, detected, fromCache := fetchState(ctx, s.Address, queryTypeFromInt(s.Type), s.IntervalSec)
	markPolled(ctx, s.ID, effectiveInterval(s, detected))
	return st, fromCache
}

func gameKindToType(kind string) query.ServerType {
	switch kind {
	case "minecraft-java":
		return query.ServerMinecraftModern
	case "minecraft-legacy":
		return query.ServerMinecraftLegacy
	case "minecraft-bedrock":
		return query.ServerMinecraftBedrock
	case "source":
		return query.ServerSource
	default:
		return query.ServerAuto
	}
}

func queryTypeFromInt(t int) query.ServerType { return query.ServerType(t) }

func intervalFor(t query.ServerType) time.Duration {
	switch t {
	case query.ServerMinecraftModern, query.ServerMinecraftLegacy, query.ServerMinecraftBedrock:
		return cfg.MinecraftInterval
	default:
		return cfg.OtherInterval
	}
}
