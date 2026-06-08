package files

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestTrafficRecordFailureBlocksAuthorizationReuse(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-fail", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 6, RangeConcurrencyLimit: 1, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	if _, err := db.Exec(`CREATE TRIGGER fail_pending_traffic
		BEFORE INSERT ON pending_traffic_events
		BEGIN SELECT RAISE(FAIL, 'traffic write failed'); END;`); err != nil {
		t.Fatal(err)
	}

	first := serveTrafficFailureRequest(handler, token)
	if first.Code != http.StatusOK || first.Body.String() != "abcdef" {
		t.Fatalf("first response should still send bytes: code=%d body=%q", first.Code, first.Body.String())
	}
	second := serveTrafficFailureRequest(handler, token)
	if second.Code != http.StatusForbidden {
		t.Fatalf("authorization with unpersisted bytes should be blocked: code=%d body=%q",
			second.Code, second.Body.String())
	}
}

func TestTrafficRecordFailureFlushesBeforeReuse(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-recover", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 6, RangeConcurrencyLimit: 1, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	if _, err := db.Exec(`CREATE TRIGGER fail_pending_traffic_once
		BEFORE INSERT ON pending_traffic_events
		BEGIN SELECT RAISE(FAIL, 'traffic write failed'); END;`); err != nil {
		t.Fatal(err)
	}
	first := serveTrafficFailureRequest(handler, token)
	if first.Code != http.StatusOK {
		t.Fatalf("first response should send bytes: code=%d body=%q", first.Code, first.Body.String())
	}
	if _, err := db.Exec(`DROP TRIGGER fail_pending_traffic_once`); err != nil {
		t.Fatal(err)
	}
	second := serveTrafficFailureRequest(handler, token)
	if second.Code != http.StatusForbidden {
		t.Fatalf("flushed exhausted authorization should be rejected: code=%d body=%q",
			second.Code, second.Body.String())
	}
	var bytes int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(sent_bytes), 0)
		FROM pending_traffic_events WHERE authorization_id = 'auth-recover'`).Scan(&bytes); err != nil {
		t.Fatal(err)
	}
	if bytes != 6 {
		t.Fatalf("pending traffic should flush before reuse, bytes=%d", bytes)
	}
}

func serveTrafficFailureRequest(handler *Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
