package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"goPinger/query"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func currentUser(r *http.Request) User { return r.Context().Value(userKey).(User) }

func decodeJSON(r *http.Request, v any) bool {
	return json.NewDecoder(r.Body).Decode(v) == nil
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func statusForErr(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func serveHTML(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, path)
}

func authPage(w http.ResponseWriter, r *http.Request) { serveHTML(w, r, "frontend/auth.html") }
func mainPage(w http.ResponseWriter, r *http.Request) { serveHTML(w, r, "frontend/app.html") }

// ------- auth (HTML flow) -------

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var u LoginUser
	if !decodeJSON(r, &u) || validate.Struct(u) != nil {
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
	deleteCookieToken(w)
	http.Redirect(w, r, "/auth", http.StatusSeeOther)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var u RegisterUser
	if !decodeJSON(r, &u) || validate.Struct(u) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := validateRegister(w, r, u); err != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

// ------- API -------

func apiMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"username": currentUser(r).Username})
}

func apiListServers(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := guardUsername(user.Username); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	list, err := listServers(r.Context(), user.Username)
	if err != nil {
		writeJSONError(w, 500, err.Error())
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
		UserID:      userID,
		Address:     in.Address,
		Type:        in.Type,
		IntervalSec: in.IntervalSec,
		Label:       in.Label,
		Enabled:     true,
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	return s
}

func apiCreateServer(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := guardUsername(user.Username); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	var in serverInput
	if !decodeJSON(r, &in) {
		writeJSONError(w, 400, "invalid json")
		return
	}
	if err := in.validate(); err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	s := in.toServer(user.Username)
	if err := insertServer(r.Context(), &s); err != nil {
		writeJSONError(w, 409, err.Error())
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
		writeJSONError(w, statusForErr(err), err.Error())
		return
	}
	var in serverInput
	if !decodeJSON(r, &in) {
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
		writeJSONError(w, 500, err.Error())
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
		writeJSONError(w, statusForErr(err), err.Error())
		return
	}
	if err := deleteServer(r.Context(), user.Username, id); err != nil {
		writeJSONError(w, 500, err.Error())
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
		writeJSONError(w, statusForErr(err), err.Error())
		return
	}
	state, detected, _ := fetchState(r.Context(), s.Address, queryTypeFromInt(s.Type))
	_ = saveState(r.Context(), s.ID, state)
	markPolled(r.Context(), s.ID, intervalFor(detected))
	publishState(user.Username, s, state)
	writeJSON(w, 200, state)
}

type queryInput struct {
	Address string `json:"address"`
	Type    int    `json:"type"`
}

func apiQuery(w http.ResponseWriter, r *http.Request) {
	var in queryInput
	if !decodeJSON(r, &in) {
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
	state, _ := queryAddressWithType(r.Context(), in.Address, query.ServerType(in.Type))
	writeJSON(w, 200, state)
}

func apiEvents(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := subscribe(user.Username)
	defer unsubscribe(user.Username, ch)

	hb := time.NewTicker(20 * time.Second)
	defer hb.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			fmt.Fprintf(w, "event: state\ndata: %s\n\n", ev)
			flusher.Flush()
		case <-hb.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ------- routes -------

func Routes(mux *http.ServeMux) {
	mux.Handle("GET /auth", Chain(http.HandlerFunc(authPage), NoCacheMiddleware, SuccessRedirectMiddleware))
	mux.HandleFunc("POST /logout", logoutHandler)
	mux.HandleFunc("POST /register", registerHandler)
	mux.HandleFunc("POST /login", loginHandler)

	mux.Handle("GET /app", Chain(http.HandlerFunc(mainPage), NoCacheMiddleware, AuthMiddleware))

	mux.Handle("GET /api/me", Chain(http.HandlerFunc(apiMe), APIAuthMiddleware))
	mux.Handle("GET /api/servers", Chain(http.HandlerFunc(apiListServers), APIAuthMiddleware))
	mux.Handle("POST /api/servers", Chain(http.HandlerFunc(apiCreateServer), APIAuthMiddleware))
	mux.Handle("PATCH /api/servers/{id}", Chain(http.HandlerFunc(apiUpdateServer), APIAuthMiddleware))
	mux.Handle("DELETE /api/servers/{id}", Chain(http.HandlerFunc(apiDeleteServer), APIAuthMiddleware))
	mux.Handle("POST /api/servers/{id}/poll", Chain(http.HandlerFunc(apiPollServer), APIAuthMiddleware))
	mux.Handle("POST /api/query", Chain(http.HandlerFunc(apiQuery), APIAuthMiddleware))
	mux.Handle("GET /api/events", Chain(http.HandlerFunc(apiEvents), APIAuthMiddleware))

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./frontend/static"))))
}
