package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const tokenLength = 32

func readUserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := pool.QueryRow(ctx, `
		SELECT username, hash, COALESCE(token, '') FROM users WHERE username = $1
	`, username).Scan(&user.Username, &user.Hash, &user.Token)
	return user, err
}

// Проверяем и токен, и срок его действия.
func readUserByToken(ctx context.Context, token string) (User, error) {
	var user User
	err := pool.QueryRow(ctx, `
		SELECT username, COALESCE(token, '') FROM users
		WHERE token = $1 AND (token_expires_at IS NULL OR token_expires_at > NOW())
	`, token).Scan(&user.Username, &user.Token)
	return user, err
}

func insertUser(ctx context.Context, user User) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO users(username, hash, token, token_expires_at)
		VALUES ($1, $2, $3, $4)
	`, user.Username, user.Hash, user.Token, time.Now().Add(400*24*time.Hour))
	return err
}

func updateUserToken(ctx context.Context, user User) error {
	_, err := pool.Exec(ctx, `
		UPDATE users SET token = $1, token_expires_at = $2 WHERE username = $3
	`, user.Token, time.Now().Add(400*24*time.Hour), user.Username)
	return err
}

func clearUserToken(ctx context.Context, token string) error {
	_, err := pool.Exec(ctx, `
		UPDATE users SET token = NULL, token_expires_at = NULL WHERE token = $1
	`, token)
	return err
}

func validateRegister(w http.ResponseWriter, r *http.Request, regUser RegisterUser) error {
	hash, err := generatePasswordHash(regUser.Password)
	if err != nil {
		return err
	}
	tokenValue, err := generateUserToken(tokenLength)
	if err != nil {
		return err
	}
	user := User{Username: regUser.Username, Hash: hash, Token: tokenValue}
	if err := insertUser(r.Context(), user); err != nil {
		return err
	}
	setCookieToken(w, r, tokenValue)
	return nil
}

func validateLogin(w http.ResponseWriter, r *http.Request, logUser LoginUser) error {
	readUser, err := readUserByUsername(r.Context(), logUser.Username)
	if err != nil {
		return err
	}
	ok, err := validatePasswordHash(logUser.Password, readUser.Hash)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("invalid password")
	}
	tokenValue, err := generateUserToken(tokenLength)
	if err != nil {
		return err
	}
	user := User{Username: logUser.Username, Token: tokenValue}
	if err := updateUserToken(r.Context(), user); err != nil {
		return err
	}
	setCookieToken(w, r, tokenValue)
	return nil
}
