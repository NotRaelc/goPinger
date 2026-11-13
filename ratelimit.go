package main

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

type rateEntry struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
}

var (
	authLimiter = &rateLimiter{entries: make(map[string]*rateEntry)}
	userLimiter = &rateLimiter{entries: make(map[string]*rateEntry)}
)

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		l.entries[key] = &rateEntry{count: 1, reset: now.Add(window)}
		return true
	}
	e.count++
	return e.count <= limit
}

func (l *rateLimiter) sweep() {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.entries {
		if now.After(e.reset) {
			delete(l.entries, k)
		}
	}
}

func startRateLimiterCleanup(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			authLimiter.sweep()
			userLimiter.sweep()
		}
	}
}

func clientIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// 10 попыток в минуту с одного IP на /login и /register.
func rateLimitAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authLimiter.allow(clientIP(r), 10, time.Minute) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// N запросов в window на пользователя. Должен идти ПОСЛЕ APIAuthMiddleware.
func rateLimitUser(limit int, window time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := currentUser(r)
			if !userLimiter.allow(user.Username, limit, window) {
				writeJSONError(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
