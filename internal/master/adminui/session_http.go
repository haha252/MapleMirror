package adminui

import (
	"net/http"
	"time"
)

func setSessionCookie(w http.ResponseWriter, token, expires string) {
	exp, _ := time.Parse(time.RFC3339Nano, expires)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/admin",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Expires: exp})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/admin",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(0, 0), MaxAge: -1})
}
