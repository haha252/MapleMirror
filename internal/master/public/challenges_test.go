package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChallengeHandlersRejectOversizedJSONBody(t *testing.T) {
	server := Server{}
	body := `{"asset_id":"` + strings.Repeat("x", publicJSONBodyLimit) + `"}`
	cases := []struct {
		name    string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "api challenge", path: "/api/public/v1/api/challenges", handler: server.apiChallenge},
		{name: "api authorization", path: "/api/public/v1/api/authorizations", handler: server.apiAuthorize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413, got %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"INVALID_REQUEST"`) {
				t.Fatalf("expected stable error code, body=%s", rec.Body.String())
			}
		})
	}
}

func TestV1WebHandlersAreRetiredBeforeParsing(t *testing.T) {
	server := Server{}
	for _, handler := range []func(http.ResponseWriter, *http.Request){server.webChallenge, server.webAuthorize} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not-json")))
		if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), `"code":"WEB_PROTOCOL_RETIRED"`) {
			t.Fatalf("V1 web retirement status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestChallengeRejectsMalformedForwardedSource(t *testing.T) {
	server := Server{TrustedCIDRs: []string{"127.0.0.0/8"}}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges",
		strings.NewReader(`{"asset_id":"asset-1"}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "invalid, 192.0.2.1")
	rec := httptest.NewRecorder()
	server.apiChallenge(rec, req)
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), `"code":"INVALID_CLIENT_SOURCE"`) {
		t.Fatalf("malformed forwarded source status=%d body=%s", rec.Code, rec.Body.String())
	}
}
