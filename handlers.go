package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"goPinger/query"
)

// ---------- json helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func currentUser(r *http.Request) User { return r.Context().Value(userKey).(User) }

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return json.NewDecoder(r.Body).Decode(v) == nil
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func statusForErr(err error) int {
	if errors.Is(err, ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("internal", "err", err, "path", r.URL.Path,
		"req_id", r.Context().Value(reqIDKey))
	writeJSONError(w, http.StatusInternalServerError, "internal server error")
}

// ---------- cookies ----------

func deleteCookieToken(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "token", Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, Secure: isSecureRequest(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Unix(0, 0),
	})
}

func setCookieToken(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: "token", Value: token, Path: "/",
		MaxAge: 400 * 24 * 60 * 60, HttpOnly: true,
		Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(400 * 24 * time.Hour),
	})
}

// ---------- pages / health ----------

func serveHTML(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, path)
}
func authPage(w http.ResponseWriter, r *http.Request) { serveHTML(w, r, "frontend/auth.html") }
func mainPage(w http.ResponseWriter, r *http.Request) { serveHTML(w, r, "frontend/app.html") }

func healthHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbOK := pool.Ping(ctx) == nil
	redisOK := redisClient.Ping(ctx).Err() == nil
	status := http.StatusOK
	if !dbOK || !redisOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]bool{"db": dbOK, "redis": redisOK})
}

// ---------- auth ----------

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var u LoginUser
	if !decodeJSON(w, r, &u) || validate.Struct(u) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := validateLogin(w, r, u); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("token"); err == nil && c.Value != "" {
		_ = clearUserToken(r.Context(), c.Value)
		clearUserCache(r.Context(), c.Value)
	}
	deleteCookieToken(w, r)
	http.Redirect(w, r, "/auth", http.StatusSeeOther)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var u RegisterUser
	if !decodeJSON(w, r, &u) || validate.Struct(u) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := validateRegister(w, r, u); err != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

// ---------- API ----------

type limitsResponse struct {
	MaxServersPerUser  int `json:"max_servers_per_user"`
	MinRealInterval    int `json:"min_real_interval"`
	MinecraftInterval  int `json:"minecraft_interval"`
	OtherInterval      int `json:"other_interval"`
	PasswordMin        int `json:"password_min"`
	MaxQueryConcurrent int `json:"max_query_concurrent"`
	SSEMax             int `json:"sse_max"`
}

func apiLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, limitsResponse{
		MaxServersPerUser:  cfg.MaxServersPerUser,
		MinRealInterval:    int(cfg.MinRealInterval.Seconds()),
		MinecraftInterval:  int(cfg.MinecraftInterval.Seconds()),
		OtherInterval:      int(cfg.OtherInterval.Seconds()),
		PasswordMin:        cfg.PasswordMinLength,
		MaxQueryConcurrent: cfg.MaxQueryConcurrent,
		SSEMax:             cfg.SSEMaxConnections,
	})
}

func apiMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"username": currentUser(r).Username})
}

func apiListServers(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, err := listServersCached(r.Context(), user.Username)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if list == nil {
		list = []ServerDTO{}
	}
	writeJSON(w, 200, list)
}

type serverInput struct {
	Address     string `json:"address"`
	Type        int    `json:"type"`
	IntervalSec int    `json:"interval_sec"`
	Label       string `json:"label"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

func (in *serverInput) validate() error {
	if err := guardAddress(in.Address); err != nil {
		return err
	}
	if err := guardServerType(in.Type); err != nil {
		return err
	}
	if err := guardLabel(in.Label); err != nil {
		return err
	}
	if in.IntervalSec < 0 {
		in.IntervalSec = 0
	}
	return nil
}

func (in serverInput) toServer(userID string) Server {
	s := Server{
		UserID: userID, Address: in.Address, Type: in.Type,
		IntervalSec: in.IntervalSec, Label: in.Label, Enabled: true,
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	return s
}

func apiCreateServer(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := guardServerQuota(r.Context(), user.Username); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	var in serverInput
	if !decodeJSON(w, r, &in) {
		writeJSONError(w, 400, "invalid json")
		return
	}
	if err := in.validate(); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	s := in.toServer(user.Username)
	if err := insertServer(r.Context(), &s); err != nil {
		writeJSONError(w, 409, "address already exists or conflicts")
		return
	}
	st, _ := bootstrapServerState(r.Context(), s)
	_ = saveState(r.Context(), s.ID, st)
	publishState(user.Username, s, st)
	writeJSON(w, 201, buildDTO(s, &st))
}

func apiUpdateServer(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, 400, "invalid id")
		return
	}
	s, err := getServerForUser(r.Context(), user.Username, id)
	if err != nil {
		writeJSONError(w, statusForErr(err), "not found")
		return
	}
	var in serverInput
	if !decodeJSON(w, r, &in) {
		writeJSONError(w, 400, "invalid json")
		return
	}
	if err := in.validate(); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	s.Address, s.Type, s.IntervalSec, s.Label = in.Address, in.Type, in.IntervalSec, in.Label
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if err := updateServer(r.Context(), s); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func apiDeleteServer(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, 400, "invalid id")
		return
	}
	if _, err := getServerForUser(r.Context(), user.Username, id); err != nil {
		writeJSONError(w, statusForErr(err), "not found")
		return
	}
	if err := deleteServer(r.Context(), user.Username, id); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func apiPollServer(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, 400, "invalid id")
		return
	}
	s, err := getServerForUser(r.Context(), user.Username, id)
	if err != nil {
		writeJSONError(w, statusForErr(err), "not found")
		return
	}

	// Защита от спама (30 запросов в минуту на пользователя)
	if !userLimiter.allow(user.Username, 30, time.Minute) {
		// Лимит превышен — пробуем отдать кеш
		if st, _, ok := getStateCache(r.Context(), s.Address); ok {
			st.FromCache = true
			writeJSON(w, 200, st)
			return
		}
		writeJSONError(w, http.StatusTooManyRequests, "too many requests, try again later")
		return
	}

	state, detected, _ := fetchState(r.Context(), s.Address, queryTypeFromInt(s.Type), s.IntervalSec)
	_ = saveState(r.Context(), s.ID, state)
	markPolled(r.Context(), s.ID, effectiveInterval(s, detected))
	publishState(user.Username, s, state)
	writeJSON(w, 200, state)
}

func apiRefreshStale(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, err := listServers(r.Context(), user.Username)
	if err != nil {
		internalError(w, r, err)
		return
	}

	var stale []ServerDTO
	for _, dto := range list {
		if !dto.Enabled {
			continue
		}
		if dto.State == nil || dto.State.CheckedAt == 0 {
			stale = append(stale, dto)
			continue
		}
		eff := time.Duration(dto.EffectiveIntervalSec) * time.Second
		if eff <= 0 {
			eff = 60 * time.Second
		}
		if time.Since(time.UnixMilli(dto.State.CheckedAt)) >= eff {
			stale = append(stale, dto)
		}
	}

	if len(stale) > 0 {
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)
		for _, dto := range stale {
			dto := dto
			wg.Add(1)
			safeGo(func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				s := Server{
					ID: dto.ID, UserID: user.Username,
					Address: dto.Address, Type: dto.Type,
					IntervalSec: dto.IntervalSec,
					Label:       dto.Label, Enabled: dto.Enabled,
				}
				state, detected, _ := fetchState(bgCtx, s.Address, queryTypeFromInt(s.Type), s.IntervalSec)
				_ = saveState(bgCtx, s.ID, state)
				markPolled(bgCtx, s.ID, effectiveInterval(s, detected))
				publishState(user.Username, s, state)
			})
		}
		safeGo(func() { wg.Wait(); cancel() })
	}

	writeJSON(w, 200, map[string]int{"refreshing": len(stale), "total": len(list)})
}

func apiPollAll(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	list, err := listServers(r.Context(), user.Username)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if len(list) > 0 {
		keys := make([]string, len(list))
		for i, dto := range list {
			keys[i] = keyNext(dto.ID)
		}
		redisClient.Del(r.Context(), keys...)
	}
	writeJSON(w, 200, map[string]int{"queued": len(list)})
}

type queryInput struct {
	Address string `json:"address"`
	Type    int    `json:"type"`
}

func apiQuery(w http.ResponseWriter, r *http.Request) {
	var in queryInput
	if !decodeJSON(w, r, &in) {
		writeJSONError(w, 400, "invalid json")
		return
	}
	if err := guardAddress(in.Address); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	if err := guardServerType(in.Type); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}

	user := currentUser(r)
	// Защита от спама (10 запросов в секунду на пользователя)
	if !userLimiter.allow(user.Username, 10, time.Second) {
		// Лимит превышен — пробуем отдать кеш
		if st, _, ok := getStateCache(r.Context(), in.Address); ok {
			st.FromCache = true
			writeJSON(w, 200, st)
			return
		}
		writeJSONError(w, http.StatusTooManyRequests, "too many requests, try again later")
		return
	}

	state, _, _ := fetchState(r.Context(), in.Address, query.ServerType(in.Type), 0)
	writeJSON(w, 200, state)
}

func apiEvents(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, 500, "streaming unsupported")
		return
	}
	ch, err := subscribe(user.Username)
	if err != nil {
		writeJSONError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	defer unsubscribe(user.Username, ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	hb := time.NewTicker(20 * time.Second)
	defer hb.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprintf(w, "event: states\ndata: %s\n\n", ev)
			flusher.Flush()
		case <-hb.C:
			_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ---------- routes ----------

func Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", healthHandler)

	mux.Handle("GET /auth", Chain(http.HandlerFunc(authPage), NoCacheMiddleware, SuccessRedirectMiddleware))
	mux.HandleFunc("POST /logout", logoutHandler)
	mux.HandleFunc("POST /register", rateLimitAuth(registerHandler))
	mux.HandleFunc("POST /login", rateLimitAuth(loginHandler))

	mux.Handle("GET /app", Chain(http.HandlerFunc(mainPage), NoCacheMiddleware, AuthMiddleware))

	mux.Handle("GET /api/me", Chain(http.HandlerFunc(apiMe), APIAuthMiddleware))
	mux.Handle("GET /api/limits", Chain(http.HandlerFunc(apiLimits), APIAuthMiddleware))

	// Убрали rateLimitUser с этого роута, чтобы спам F5 не скрывал сервера
	mux.Handle("GET /api/servers", Chain(http.HandlerFunc(apiListServers), APIAuthMiddleware))

	mux.Handle("POST /api/servers", Chain(http.HandlerFunc(apiCreateServer),
		APIAuthMiddleware, rateLimitUser(10, time.Minute)))
	mux.Handle("PATCH /api/servers/{id}", Chain(http.HandlerFunc(apiUpdateServer),
		APIAuthMiddleware, rateLimitUser(30, time.Minute)))
	mux.Handle("DELETE /api/servers/{id}", Chain(http.HandlerFunc(apiDeleteServer),
		APIAuthMiddleware, rateLimitUser(30, time.Minute)))

	// Убрали rateLimitUser с этих роутов — они легкие и уже защищены кешем
	mux.Handle("POST /api/servers/{id}/poll", Chain(http.HandlerFunc(apiPollServer), APIAuthMiddleware))
	mux.Handle("POST /api/servers/poll-all", Chain(http.HandlerFunc(apiPollAll), APIAuthMiddleware))
	mux.Handle("POST /api/servers/refresh-stale", Chain(http.HandlerFunc(apiRefreshStale), APIAuthMiddleware))

	// Оставили лимит только на произвольный query, так как это прямой запрос к левому серверу
	mux.Handle("POST /api/query", Chain(http.HandlerFunc(apiQuery), APIAuthMiddleware))

	mux.Handle("GET /api/events", Chain(http.HandlerFunc(apiEvents), APIAuthMiddleware))

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./frontend/static"))))
}
