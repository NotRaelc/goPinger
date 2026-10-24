package main

import (
	"net/http"
	"time"
)

func deleteCookieToken(w http.ResponseWriter) {
	expiredCookie := &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
	}
	http.SetCookie(w, expiredCookie)
}

func setCookieToken(w http.ResponseWriter, tokenValue string) {
	expiredCookie := &http.Cookie{ // Лимит браузера - 400 дней
		Name:     "token",
		Value:    tokenValue,
		Path:     "/",
		MaxAge:   400 * 24 * 60 * 60,
		HttpOnly: true,
		Expires:  time.Now().Add(400 * 24 * time.Hour),
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, expiredCookie)
}
