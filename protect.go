package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"goPinger/query"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

func guardAddress(a string) error {
	a = strings.TrimSpace(a)
	if a == "" || len(a) > 255 {
		return fmt.Errorf("invalid address")
	}
	if _, _, err := query.SplitHostPort(a); err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}
	return nil
}

func guardServerType(t int) error {
	if t < int(query.ServerAuto) || t > int(query.ServerMinecraftBedrock) {
		return fmt.Errorf("invalid server type: %d", t)
	}
	return nil
}

func guardLabel(l string) error {
	if len(l) > 255 {
		return fmt.Errorf("label too long")
	}
	return nil
}

func guardServerQuota(ctx context.Context, userID string) error {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE user_id = $1`, userID).Scan(&n)
	if err != nil {
		return err
	}
	if n >= cfg.MaxServersPerUser {
		return fmt.Errorf("server limit reached (%d)", cfg.MaxServersPerUser)
	}
	return nil
}

// Возвращаем 404 и для «не найден», и для «чужой» — чтобы не палить ID.
func getServerForUser(ctx context.Context, userID string, id int64) (Server, error) {
	if userID == "" || id <= 0 {
		return Server{}, ErrNotFound
	}
	s, err := getServerByID(ctx, id)
	if err != nil {
		return s, err
	}
	if s.UserID != userID {
		return Server{}, ErrNotFound
	}
	return s, nil
}
