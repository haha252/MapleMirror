package files

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestHandlerServesHEADWithoutActivatingAuthorization(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-head", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-head"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodHead, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD should return metadata: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Length"); got != "6" {
		t.Fatalf("HEAD should expose file size through Content-Length, got %q", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD should not write body, got %q", rec.Body.String())
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_authorizations
		WHERE authorization_id = 'auth-head'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("HEAD metadata should not activate local authorization: rows=%d err=%v", rows, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_traffic_events
		WHERE authorization_id = 'auth-head'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("HEAD metadata should not record traffic: rows=%d err=%v", rows, err)
	}
}
