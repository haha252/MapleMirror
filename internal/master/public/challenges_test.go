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
		{name: "web challenge", path: "/api/public/v1/web/challenges", handler: server.webChallenge},
		{name: "api challenge", path: "/api/public/v1/api/challenges", handler: server.apiChallenge},
		{name: "web authorization", path: "/api/public/v1/web/authorizations", handler: server.webAuthorize},
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
