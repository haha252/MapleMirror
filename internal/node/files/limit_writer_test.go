package files

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestHandlerStopsAtTrafficLimitBytes(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, TrafficLimitBytes: 4, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Body.String() != "abcd" {
		t.Fatalf("下载节点应在流量上限处停止发送：code=%d body=%q", rec.Code, rec.Body.String())
	}
	var bytes int64
	err = db.QueryRow(`SELECT COALESCE(SUM(sent_bytes), 0)
		FROM pending_traffic_events WHERE authorization_id = 'auth-1'`).Scan(&bytes)
	if err != nil || bytes != 4 {
		t.Fatalf("应只记录实际写出的上限字节：bytes=%d err=%v", bytes, err)
	}
}

func TestClaimAuthorizationBytesSharesBudget(t *testing.T) {
	handler := &Handler{}
	if got := handler.claimAuthorizationBytes("auth-1", 4, 0, 3); got != 3 {
		t.Fatalf("first claim = %d, want 3", got)
	}
	if got := handler.claimAuthorizationBytes("auth-1", 4, 0, 3); got != 1 {
		t.Fatalf("second claim should share remaining budget: %d", got)
	}
	if got := handler.claimAuthorizationBytes("auth-1", 4, 0, 1); got != 0 {
		t.Fatalf("exhausted claim = %d, want 0", got)
	}
}

func TestLimitCountingWriterSplitsTrafficCheckpoints(t *testing.T) {
	db, _, _ := prepareNodeFile(t)
	handler := &Handler{DB: db}
	claims := downloadtoken.Claims{AuthorizationID: "auth-1", RequestID: "master-req-1"}
	counter := &limitCountingWriter{
		ResponseWriter: httptest.NewRecorder(), handler: handler,
		authorizationID: claims.AuthorizationID, claims: claims,
		limit: trafficCheckpointBytes*2 + 1, assetID: "asset-1",
		nodeRequestID: "node-req-1", lastCheckpointAt: time.Now().UTC(),
	}
	if _, err := counter.Write(bytes.Repeat([]byte{'x'}, int(trafficCheckpointBytes)+1)); err != nil {
		t.Fatal(err)
	}
	counter.flushTrafficCheckpoint(true)
	var events, total int64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(sent_bytes), 0)
		FROM pending_traffic_events`).Scan(&events, &total); err != nil {
		t.Fatal(err)
	}
	if events != 2 || total != trafficCheckpointBytes+1 {
		t.Fatalf("checkpoint events=%d bytes=%d", events, total)
	}
}
