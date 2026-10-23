package main

import (
	"encoding/json"
	"net/http"
)

func loginHandler(w http.ResponseWriter, r *http.Request) {
	logUser := LoginUser{}
	if err := json.NewDecoder(r.Body).Decode(&logUser); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := validate.Struct(logUser); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Далее сверка с базой данных по логину, хешу пароля и сроку токена
	// Если по базе не прошли, то http.StatusUnauthorized
	// Заменяем новым токеном в базе данных
	// Если окей, то пропускаем на страницу. Если нет, то BadRequest
	err := validateLogin(w, r, logUser)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	deleteCookieToken(w)
	http.Redirect(w, r, "/auth", http.StatusSeeOther)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	regUser := RegisterUser{}
	if err := json.NewDecoder(r.Body).Decode(&regUser); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := validate.Struct(regUser); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Генерим хеш пароля, оставляем имя пользователя
	// Далее вставляем всё это в базу
	// В базу также вставляем токен и его срок
	// Если в базе уже есть юзер, то StatusConflict

	err := validateRegister(w, r, regUser)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}

	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func authPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, "frontend/auth.html")
}

func mainPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, "frontend/app.html")
}

func Routes(mux *http.ServeMux) {
	// публичные
	mux.Handle("GET /auth", Chain(
		http.HandlerFunc(authPage),
		NoCacheMiddleware,
		SuccessRedirectMiddleware,
	))

	mux.HandleFunc("POST /logout", logoutHandler)
	mux.HandleFunc("POST /register", registerHandler)
	mux.HandleFunc("POST /login", loginHandler)

	// защищённые
	mux.Handle("GET /app", Chain(
		http.HandlerFunc(mainPage),
		NoCacheMiddleware,
		AuthMiddleware,
	))

	mux.Handle("/static/", http.StripPrefix("/static/",
		http.FileServer(http.Dir("./frontend/static"))))
}
