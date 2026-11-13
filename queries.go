package main

import (
	"context"
	"log/slog"
	"time"

	"goPinger/query"
)

// queryAddressWithType — низкоуровневая попытка опроса с учётом explicit-типа.
// Не трогает rate-limit/lock — их обеспечивает fetchState.
func queryAddressWithType(ctx context.Context, address string, explicit query.ServerType) (ServerState, query.ServerType) {
	qctx, cancel := context.WithTimeout(ctx, cfg.QueryTimeout)
	defer cancel()

	if explicit != query.ServerAuto {
		resp := query.QueryServerOpts(qctx, address, query.Options{Type: explicit})
		if resp.Error == nil {
			cacheType(ctx, address, explicit)
			return stateOK(resp.Payload), explicit
		}
		slog.Debug("explicit type failed, re-detecting",
			"address", address, "type", explicit.String(), "err", resp.Error)
	}

	if t, ok := getCachedType(ctx, address); ok && t != explicit {
		resp := query.QueryServerOpts(qctx, address, query.Options{Type: t})
		if resp.Error == nil {
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

// fetchState — единственная точка реальных запросов к серверам.
// Rate-limit глобальный на адрес; конкурентные вызовы ждут кеш, а не плодят запросы.
func fetchState(ctx context.Context, address string, explicit query.ServerType) (ServerState, query.ServerType, bool) {
	if last, ok := getLastPoll(ctx, address); ok && time.Since(last) < cfg.MinRealInterval {
		if st, _, ok := getStateCache(ctx, address); ok {
			st.FromCache = true
			return *st, gameKindToType(st.Payload.GameKind), true
		}
		if st, ok := waitForCache(ctx, address); ok {
			st.FromCache = true
			return *st, gameKindToType(st.Payload.GameKind), true
		}
	}

	release, ok := acquireServerLock(ctx, address)
	if !ok {
		if st, ok := waitForCache(ctx, address); ok {
			st.FromCache = true
			return *st, gameKindToType(st.Payload.GameKind), true
		}
		return ServerState{
			OK:        false,
			Error:     "server is busy, try again",
			CheckedAt: time.Now().UnixMilli(),
		}, query.ServerAuto, false
	}
	defer release()

	if last, ok := getLastPoll(ctx, address); ok && time.Since(last) < cfg.MinRealInterval {
		if st, _, ok := getStateCache(ctx, address); ok {
			st.FromCache = true
			return *st, gameKindToType(st.Payload.GameKind), true
		}
	}

	state, detected := queryAddressWithType(ctx, address, explicit)
	saveStateCache(ctx, address, &state, intervalFor(detected))
	slog.Debug("real query", "address", address, "type", detected.String(), "ok", state.OK)
	return state, detected, false
}

func bootstrapServerState(ctx context.Context, s Server) (ServerState, bool) {
	ourType := queryTypeFromInt(s.Type)
	ourInterval := intervalFor(ourType)

	if cached, remaining, ok := getStateCache(ctx, s.Address); ok && cached.Payload != nil {
		detected := gameKindToType(cached.Payload.GameKind)
		matches := ourType == query.ServerAuto || ourType == detected
		if matches {
			effective := ourInterval
			if ourType == query.ServerAuto {
				effective = intervalFor(detected)
			}
			if effective >= remaining {
				extendStateCache(ctx, s.Address, effective)
				markPolled(ctx, s.ID, effective)
				cached.FromCache = true
				return *cached, true
			}
		}
	}

	st, detected, fromCache := fetchState(ctx, s.Address, ourType)
	markPolled(ctx, s.ID, intervalFor(detected))
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
	case query.ServerSource:
		return cfg.OtherInterval
	case query.ServerMinecraftModern, query.ServerMinecraftLegacy, query.ServerMinecraftBedrock:
		return cfg.MinecraftInterval
	default:
		return cfg.OtherInterval
	}
}
