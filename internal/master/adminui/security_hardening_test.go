package adminui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mirror-server/internal/config"
)

func TestAdminResponsesSetSecurityHeaders(t *testing.T) {
	server, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
	if rec.Header().Get("Cache-Control") != "no-store" ||
		rec.Header().Get("X-Frame-Options") != "DENY" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("management response is missing security headers: %#v", rec.Header())
	}
}

func TestConcurrentLoginFailuresPersistEveryAttempt(t *testing.T) {
	server, db := newTestServer(t)
	const attempts = 6
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- server.store.recordFailure(context.Background(), "192.0.2.25")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var failures, blocks int
	if err := db.QueryRow(`SELECT failed_count FROM admin_login_failures`).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM admin_ip_blocks`).Scan(&blocks); err != nil {
		t.Fatal(err)
	}
	if failures != attempts || blocks != 1 {
		t.Fatalf("concurrent failures=%d blocks=%d", failures, blocks)
	}
}

func TestLoginRejectsOversizedBody(t *testing.T) {
	server, _ := newTestServer(t)
	body := strings.Repeat("x", int(adminLoginBodyLimit)+1)
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized login status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginRejectsMalformedForwardedSource(t *testing.T) {
	server, _ := newProxyTestServer(t, []string{"127.0.0.0/8"}, config.AdminWeb{})
	req := loginForm("admin", "correct-password")
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("X-Forwarded-For", "invalid, 192.0.2.1")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || sessionFrom(rec.Result().Cookies()) != nil {
		t.Fatalf("malformed forwarded login status=%d", rec.Code)
	}
}
