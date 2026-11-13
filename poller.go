package main

import (
	"context"
	"log/slog"
	"time"
)

func startPoller(ctx context.Context) {
	sem := make(chan struct{}, cfg.MaxQueryConcurrent)
	ticker := time.NewTicker(cfg.PollTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollDue(ctx, sem)
		}
	}
}

func pollDue(ctx context.Context, sem chan struct{}) {
	servers, err := enabledServersCached(ctx)
	if err != nil {
		slog.Error("list enabled servers", "err", err)
		return
	}

	for _, s := range servers {
		if !isDue(ctx, s.ID) || !acquirePoll(ctx, s.ID) {
			continue
		}
		select {
		case sem <- struct{}{}:
			go func(s Server) {
				defer func() { <-sem }()
				pollServer(ctx, s)
			}(s)
		default:
			// семафор полон — остальное подхватит следующий тик
		}
	}
}

func pollServer(ctx context.Context, s Server) {
	state, detected, _ := fetchState(ctx, s.Address, queryTypeFromInt(s.Type))
	if err := saveState(ctx, s.ID, state); err != nil {
		slog.Error("save state", "server_id", s.ID, "err", err)
	}
	markPolled(ctx, s.ID, intervalFor(detected))
	publishState(s.UserID, s, state)
}
