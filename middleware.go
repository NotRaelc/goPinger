package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"
	"uuid"
)

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// список разрешённых origin'ов
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

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "600")

		// preflight-запрос
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slog.Info(fmt.Sprintf("-> %s %s", r.Method, r.RequestURI))
		next.ServeHTTP(w, r)
		slog.Info(fmt.Sprintf("<- %s %s (%d ms)", r.Method, r.RequestURI, time.Since(start).Milliseconds()))
	})
}

func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error(fmt.Sprintf("%v", err))
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

		ctx := context.WithValue(r.Context(), "req_id", reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCookie, cookieErr := r.Cookie("token")
		if cookieErr != nil || tokenCookie.Value == "" {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		// Далее сверка с базой данных
		// И если нет, то сбрасываем
		// А если да, то пропускаем
		_, readErr := readUsernameByToken(r.Context(), tokenCookie.Value)
		if readErr != nil {
			deleteCookieToken(w) // TODO: Use another lib with validation
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SuccessRedirectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCookie, cookieErr := r.Cookie("token")
		if cookieErr != nil || tokenCookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Далее сверка с базой данных
		// И если нет, то сбрасываем
		// А если да, то пропускаем
		_, readErr := readUsernameByToken(r.Context(), tokenCookie.Value)
		if readErr == nil {
			http.Redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func NoCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache") // для очень старых прокси
		w.Header().Set("Expires", "0")       // то же
		next.ServeHTTP(w, r)
	})
}

type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for _, fn := range slices.Backward(middlewares) {
		h = fn(h)
	}
	return h
}
