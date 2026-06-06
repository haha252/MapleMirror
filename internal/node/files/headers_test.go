package files

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestHandlerSetsNoReferrerPolicyForQueryTokenDownload(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/p1/v1/a.zip?token="+token, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()

	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
}
