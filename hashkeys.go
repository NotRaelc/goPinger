package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Параметры из второго рекомендованного профиля RFC 9106.
const (
	argonTime    = 3         // Количество проходов (итераций)
	argonMemory  = 64 * 1024 // 64 MiB в KiB
	argonThreads = 4         // Параллелизм (количество потоков)
	argonKeyLen  = 32        // Длина итогового ключа (32 байта = 256 бит)
	saltLen      = 16        // Длина соли (16 байт)
)

// Вайбкод, это не что-то уникальное в проектировании ;)
func generatePasswordHash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	encodedSalt := b64.EncodeToString(salt)
	encodedKey := b64.EncodeToString(key)

	// Формат: $argon2id$v=19$m=...,t=...,p=...$salt$hash
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, encodedSalt, encodedKey,
	), nil
}

func validatePasswordHash(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("неверный формат хеша: ожидается argon2id")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("неподдерживаемая версия Argon2")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	originalHash, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	computedHash := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(originalHash)))
	if subtle.ConstantTimeCompare(computedHash, originalHash) == 1 {
		return true, nil
	}
	return false, nil
}

func generateUserToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
