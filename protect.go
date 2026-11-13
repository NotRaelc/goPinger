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

// Валидация входных данных до любого похода в БД.
func guardUsername(u string) error {
	if u == "" || len(u) > 32 {
		return fmt.Errorf("invalid username")
	}
	return nil
}

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

// Проверка владения сервером до любого изменения. Возвращает 404/403 без лишних чтений.
func getServerForUser(ctx context.Context, userID string, id int64) (Server, error) {
	if userID == "" || id <= 0 {
		return Server{}, ErrNotFound
	}
	s, err := getServerByID(ctx, id)
	if err != nil {
		return s, err
	}
	if s.UserID != userID {
		return Server{}, ErrForbidden
	}
	return s, nil
}