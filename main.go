package main

import (
	"context"
	"fmt"

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
	ctx := context.Background()
	if err := initDb(ctx); err != nil {
		panic(err)
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

	serverErr := http.ListenAndServe(":8080", handler)
	if serverErr != nil {
		panic(serverErr)
	}
	fmt.Println(os.Getenv("DATABASE_URL"))
}
