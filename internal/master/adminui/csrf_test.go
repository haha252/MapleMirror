package adminui

import (
	"net/http"
	"net/http/httptest"
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

func testAdminWeb() config.AdminWeb {
	return config.AdminWeb{}
}
