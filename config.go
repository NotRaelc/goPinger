package main

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL        string
	RedisURL           string
	ListenAddr         string
	PollTick           time.Duration
	MinecraftInterval  time.Duration
	OtherInterval      time.Duration
	QueryTimeout       time.Duration
	MaxQueryConcurrent int
	UserCacheTTL       time.Duration
	PollLockTTL        time.Duration
	MinRealInterval    time.Duration // минимальный глобальный интервал реального запроса на адрес
	LockWaitTimeout    time.Duration // сколько ждать чужой lock при отсутствии кеша
}

const (
	minInterval = 10 * time.Second
	maxInterval = time.Hour
)

func LoadConfig() Config {
	return Config{
		DatabaseURL:        env("DATABASE_URL", ""),
		RedisURL:           env("REDIS_URL", "redis://localhost:6379/0"),
		ListenAddr:         env("LISTEN_ADDR", ":8080"),
		PollTick:           clampDuration(envDur("POLL_TICK", 5*time.Second), time.Second, time.Minute),
		MinecraftInterval:  clampDuration(envDur("MC_INTERVAL", 3*time.Minute), minInterval, maxInterval),
		OtherInterval:      clampDuration(envDur("OTHER_INTERVAL", 10*time.Second), 5*time.Second, maxInterval),
		QueryTimeout:       clampDuration(envDur("QUERY_TIMEOUT", 5*time.Second), time.Second, 30*time.Second),
		MaxQueryConcurrent: clampInt(envInt("MAX_QUERY_CONCURRENT", 10), 1, 200),
		UserCacheTTL:       envDur("USER_CACHE_TTL", 5*time.Minute),
		PollLockTTL:        envDur("POLL_LOCK_TTL", 30*time.Second),
		MinRealInterval:    clampDuration(envDur("MIN_REAL_INTERVAL", 5*time.Second), time.Second, time.Minute),
		LockWaitTimeout:    clampDuration(envDur("LOCK_WAIT_TIMEOUT", 3*time.Second), 500*time.Millisecond, 30*time.Second),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func clampDuration(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}