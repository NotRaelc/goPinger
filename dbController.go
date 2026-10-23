package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

const tokenLength = 32

func initDbPageUsers(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
	CREATE TABLE IF NOT EXISTS users (
                       username VARCHAR NOT NULL,
                       hash VARCHAR NOT NULL,
                       token VARCHAR,
                       /*token_expires_at TIME,*/

                       UNIQUE(username))
`)
	return err
}

func readUserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := pool.QueryRow(ctx, `
	SELECT username, hash, token FROM users
	WHERE username = $1
`, username).Scan(&user.Username, &user.Hash, &user.Token)
	return user, err
}

func readUserByToken(ctx context.Context, token string) (User, error) {
	var user User
	err := pool.QueryRow(ctx, `
	SELECT username, hash, token FROM users
	WHERE token = $1
`, token).Scan(&user.Username, &user.Hash, &user.Token)
	return user, err
}

func insertUser(ctx context.Context, user User) error {
	_, err := pool.Exec(ctx, `
	INSERT INTO users(username, hash, token) VALUES ($1, $2, $3)
`, user.Username, user.Hash, user.Token)
	return err
}

func updateUserToken(ctx context.Context, user User) error {
	_, err := pool.Exec(ctx, `
	UPDATE users SET token = $1 WHERE username = $2
`, user.Token, user.Username)
	return err
}

func validateRegister(w http.ResponseWriter, r *http.Request, regUser RegisterUser) error {
	hash, hashError := generatePasswordHash(regUser.Password)
	if hashError != nil {
		return hashError
	}
	tokenValue, tokenErr := generateUserToken(tokenLength)
	if tokenErr != nil {
		return tokenErr
	}
	user := User{Username: regUser.Username, Hash: hash, Token: tokenValue}
	err := insertUser(r.Context(), user)
	if err != nil {
		return err
	}
	setCookieToken(w, tokenValue)
	return nil
}

func validateLogin(w http.ResponseWriter, r *http.Request, logUser LoginUser) error {
	readUser, readUserErr := readUserByUsername(r.Context(), logUser.Username)
	if readUserErr != nil {
		return readUserErr
	}
	hashValidation, hashErr := validatePasswordHash(logUser.Password, readUser.Hash)
	if hashErr != nil {
		return hashErr
	} else if !hashValidation {
		return fmt.Errorf("invalid password")
	}
	tokenValue, tokenErr := generateUserToken(tokenLength)
	if tokenErr != nil {
		return tokenErr
	}
	user := User{Username: logUser.Username, Token: tokenValue}
	updateUserErr := updateUserToken(r.Context(), user) // Just updating token. TODO: Store tokens with Redis.
	if updateUserErr != nil {
		return updateUserErr
	}
	setCookieToken(w, tokenValue)
	return nil
}
