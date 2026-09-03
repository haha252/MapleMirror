package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorizationStatusV2RouteReturnsNodeName(t *testing.T) {
	server, authID, token := prepareAuthorizationStatus(t)
	req := httptest.NewRequest(http.MethodGet, "/api/public/v2/authorizations/"+authID, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"node_name":"节点一"`,
		`"node_id":"节点一"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in V2 authorization status: %s", want, body)
		}
	}
}
