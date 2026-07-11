package adminui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestAdminAPIWriteRequiresCSRFToken(t *testing.T) {
	server, db := newProxyTestServer(t, nil, testAdminWeb())
	cookie := loginCookie(t, server, "127.0.0.1:55000", "")

	req := httptest.NewRequest(http.MethodPost, "/admin/api/pairing-codes",
		strings.NewReader(`{"ttl_seconds":300}`))
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertCountAdminUI(t, db, "pairing_codes", 0)

	req = httptest.NewRequest(http.MethodPost, "/admin/api/pairing-codes",
		strings.NewReader(`{"ttl_seconds":300}`))
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, server.csrfToken(cookie.Value))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid csrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertCountAdminUI(t, db, "pairing_codes", 1)
}

func TestLogoutRequiresPostAndCSRF(t *testing.T) {
	server, _ := newProxyTestServer(t, nil, testAdminWeb())
	cookie := loginCookie(t, server, "127.0.0.1:55000", "")

	get := httptest.NewRequest(http.MethodGet, "/admin/logout", nil)
	get.RemoteAddr = "127.0.0.1:55000"
	get.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, get)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET logout status=%d", rec.Code)
	}

	form := url.Values{csrfFormField: {server.csrfToken(cookie.Value)}}
	post := httptest.NewRequest(http.MethodPost, "/admin/logout", strings.NewReader(form.Encode()))
	post.RemoteAddr = "127.0.0.1:55000"
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, post)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("POST logout status=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}

	check := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	check.RemoteAddr = "127.0.0.1:55000"
	check.AddCookie(cookie)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, check)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("deleted session status=%d", rec.Code)
	}
}

func testAdminWeb() config.AdminWeb {
	return config.AdminWeb{}
}
