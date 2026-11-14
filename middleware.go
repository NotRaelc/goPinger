package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type ctxKey string

const (
	userKey  ctxKey = "user"
	reqIDKey ctxKey = "req_id"
)

var reqIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// isSecureRequest — единственное место решения про HTTPS.
//
//	PROTOCOL=https → всегда true (за прокси без заголовка)
//	PROTOCOL=http  → всегда false (явно выключили)
//	PROTOCOL=auto  → r.TLS или X-Forwarded-Proto
func isSecureRequest(r *http.Request) bool {
	switch cfg.Protocol {
	case "https":
		return true
	case "http":
		return false
	}
	if r.TLS != nil {
		return true
	}
	return cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req"
	}
	return hex.EncodeToString(b[:])
}

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		switch r.Header.Get("Origin") {
		case "http://localhost:3000", "http://localhost:8080",
			"https://localhost:3000", "https://localhost:8080":
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if r.URL.Path == "/app" || r.URL.Path == "/auth" {
			h.Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data: https://cdn.cloudflare.steamstatic.com; "+
					"script-src 'self' 'unsafe-inline'; "+
					"style-src 'self' 'unsafe-inline'; connect-src 'self'; "+
					"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		slog.Info("http", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "dur_ms", time.Since(start).Milliseconds(),
			"req_id", r.Context().Value(reqIDKey))
	})
}

func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic", "err", fmt.Sprintf("%v", err), "path", r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if !reqIDRe.MatchString(reqID) {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqIDKey, reqID)))
	})
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("token")
		if err != nil || c.Value == "" {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		user, err := cachedUserToken(r.Context(), c.Value)
		if err != nil {
			deleteCookieToken(w, r)
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func APIAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("token")
		if err != nil || c.Value == "" {
			writeJSONError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		user, err := cachedUserToken(r.Context(), c.Value)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func SuccessRedirectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("token")
		if err == nil && c.Value != "" {
			if _, err := cachedUserToken(r.Context(), c.Value); err == nil {
				http.Redirect(w, r, "/app", http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func NoCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

// ---------- rate limit ----------

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

func rateLimitAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !authLimiter.allow(ip, 10, time.Minute) {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

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

// ---------- gzip ----------

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

type gzipWriter struct {
	http.ResponseWriter
	zw          *gzip.Writer
	wroteHeader bool
	skip        bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	if code < 200 || code == http.StatusNoContent || code == http.StatusNotModified ||
		(code >= 300 && code < 400) {
		g.skip = true
		g.ResponseWriter.WriteHeader(code)
		return
	}
	h := g.ResponseWriter.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Set("Vary", "Accept-Encoding")
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.skip {
		return g.ResponseWriter.Write(b)
	}
	return g.zw.Write(b)
}

func (g *gzipWriter) Flush() {
	if !g.skip {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			strings.HasPrefix(r.URL.Path, "/api/events") ||
			strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		zw := gzipPool.Get().(*gzip.Writer)
		zw.Reset(w)
		gw := &gzipWriter{ResponseWriter: w, zw: zw}
		next.ServeHTTP(gw, r)
		if !gw.skip {
			_ = zw.Close()
		}
		gzipPool.Put(zw)
	})
}

type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, ms ...Middleware) http.Handler {
	for _, fn := range slices.Backward(ms) {
		h = fn(h)
	}
	return h
}
