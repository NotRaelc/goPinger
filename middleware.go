package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"
)

type ctxKey string

const (
	userKey  ctxKey = "user"
	reqIDKey ctxKey = "req_id"
)

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := map[string]bool{
			"http://localhost:3000":  true,
			"http://localhost:8080":  true,
			"https://localhost:8080": true,
			"https://localhost:3000": true,
		}
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
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
		if reqID == "" {
			reqID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), reqIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
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
			deleteCookieToken(w)
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
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
		ctx := context.WithValue(r.Context(), userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func SuccessRedirectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("token")
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := cachedUserToken(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/app", http.StatusSeeOther)
			return
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

// ---------- Gzip ----------

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

type gzipWriter struct {
	http.ResponseWriter
	zw          *gzip.Writer
	wroteHeader bool
	skip        bool
}

// WriteHeader выставляет Content-Encoding ДО того, как заголовки уйдут клиенту.
// Иначе браузер получит gzip-тело без заголовка и покажет мусор.
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
