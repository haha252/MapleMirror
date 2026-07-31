package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV1SwitchAndRetiredWebRoutes(t *testing.T) {
	disabled := false
	server := Server{Store: Store{DB: openMaster(t)}, APIV1Enabled: &disabled}
	handler := server.Handler()
	for _, path := range []string{"/api/public/v1/api/challenges", "/api/public/v1/api/authorizations"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), `"code":"API_VERSION_RETIRED"`) {
			t.Fatalf("V1 switch path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("missing no-store: %s", path)
		}
	}
	for _, path := range []string{"/api/public/v1/web/challenges", "/api/public/v1/web/authorizations"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), `"code":"WEB_PROTOCOL_RETIRED"`) {
			t.Fatalf("retired web path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/v1/authorizations/missing", nil))
	if rec.Code == http.StatusGone {
		t.Fatal("V1 authorization status must remain available")
	}
}

func TestV2StatusAliasAndRoutesUseNoStore(t *testing.T) {
	server := Server{Store: Store{DB: openMaster(t)}}
	handler := server.Handler()
	for _, path := range []string{"/api/public/v2/web/challenges", "/api/public/v2/api/challenges",
		"/api/public/v2/web/authorizations", "/api/public/v2/api/authorizations"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		if !strings.Contains(rec.Body.String(), `"status":"error"`) {
			t.Fatalf("V2 route missing: %s", path)
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("missing no-store: %s", path)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/v2/authorizations/missing", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("V2 status alias status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}
