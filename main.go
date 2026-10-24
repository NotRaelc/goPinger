package main

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"

	"net/http"
	"os"
)

func initDb(ctx context.Context) error {
	mPool, dbErr := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if dbErr != nil {
		return dbErr
	}

	if dbPingErr := mPool.Ping(ctx); dbPingErr != nil {
		return dbPingErr
	}
	pool = mPool
	checkNames := []string{"users"}
	if checkPages := checkDbPages(ctx, checkNames...); checkPages != nil {
		return checkPages
	}

	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if dbInitErr := initDb(ctx); dbInitErr != nil {
		panic(dbInitErr)
	}
	validate = validator.New(validator.WithRequiredStructEnabled())

	mux := http.NewServeMux()
	Routes(mux)

	handler := Chain(mux,
		RecoverMiddleware,   // самый внешний: ловит паники
		RequestIDMiddleware, // X-Request-ID
		LoggingMiddleware,   // лог запроса
		CORSMiddleware,      // CORS/preflight (внутри логов)
	)
	srv := &http.Server{ // Нашел оптимизированный сервер
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,  // ← ключевой: время на чтение заголовков
		ReadTimeout:       15 * time.Second, // ← всё тело запроса целиком
		WriteTimeout:      15 * time.Second, // ← запись ответа
		IdleTimeout:       60 * time.Second, // ← keep-alive простой
		MaxHeaderBytes:    1 << 20,          // ← 1 MiB на заголовки (защита от header-flood)
	}

	go func() {
		if serverErr := srv.ListenAndServe(); serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			slog.Error("HTTP server error", "err", serverErr)
		}
	}()

	<-ctx.Done()
	slog.Warn("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Graceful shutdown failed, forcing close", "err", err)
		if closeErr := srv.Close(); closeErr != nil {
			slog.Error("Force close failed", "err", closeErr)
		}
	}

	slog.Info("Stopped")
}
