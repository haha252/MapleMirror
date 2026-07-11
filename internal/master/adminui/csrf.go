package adminui

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

const csrfHeader = "X-CSRF-Token"
const csrfFormField = "csrf_token"

type csrfTokenKey struct{}

func (s *Server) csrfToken(sessionToken string) string {
	mac := hmac.New(sha256.New, s.store.secret)
	_, _ = mac.Write([]byte("admin-csrf:" + sessionToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request, sessionToken string) bool {
	if !adminAPIWrite(r) {
		return true
	}
	got := r.Header.Get(csrfHeader)
	if got == "" && r.URL.Path == "/admin/logout" {
		r.Body = http.MaxBytesReader(w, r.Body, adminLoginBodyLimit)
		if err := r.ParseForm(); err == nil {
			got = r.PostForm.Get(csrfFormField)
		}
	}
	want := s.csrfToken(sessionToken)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "请求缺少有效 CSRF token"})
		return false
	}
	return true
}

func adminAPIWrite(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return strings.HasPrefix(r.URL.Path, "/admin/api/") || r.URL.Path == "/admin/logout"
	}
}
