package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"
)

var cfg Config

func initDb(ctx context.Context) error {
	pgCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	pgCfg.MaxConns = 20
	pgCfg.MinConns = 2
	pgCfg.MaxConnLifetime = 30 * time.Minute
	pgCfg.MaxConnIdleTime = 5 * time.Minute
	pgCfg.HealthCheckPeriod = 30 * time.Second

	p, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return err
	}
	pool = p
	return requireTables(ctx, "users", "servers", "server_states")
}

func requireTables(ctx context.Context, names ...string) error {
	for _, n := range names {
		var ok bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, n).Scan(&ok)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("required table %q missing - apply migrations", n)
		}
	}
	return nil
}

func main() {
	cfg = LoadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := initDb(ctx); err != nil {
		slog.Error("DB init failed", "err", err)
		os.Exit(1)
	}
	if err := initRedis(ctx, cfg.RedisURL); err != nil {
		slog.Error("Redis init failed. Install & start Redis", "err", err)
		os.Exit(1)
	}

	loadIconFiles()

	validate = validator.New(validator.WithRequiredStructEnabled())

	go startPoller(ctx)
	go startRateLimiterCleanup(ctx)

	mux := http.NewServeMux()
	Routes(mux)

	handler := Chain(mux,
		RecoverMiddleware,
		RequestIDMiddleware,
		LoggingMiddleware,
		CORSMiddleware,
		GzipMiddleware,
	)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		slog.Info("HTTP listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Warn("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		_ = srv.Close()
	}
	if redisClient != nil {
		_ = redisClient.Close()
	}
	if pool != nil {
		pool.Close()
	}
	slog.Info("stopped")
}
