package files

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/swarm"
)

func TestHandlerPublicDownloadUsesTrafficLimiter(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := testDownloadClaims()
	token, _ := signer.Sign(claims)
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	limiter := &countingLimiter{}
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1",
		Signer: signer, TrafficLimiter: limiter}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || limiter.bytes != 6 {
		t.Fatalf("public download limiter bytes=%d code=%d",
			limiter.bytes, rec.Code)
	}
}

func TestHandlerReplicationUsesTrafficLimiter(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	token := signReplicationToken(t, signer, testReplicationClaims())
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	limiter := &countingLimiter{}
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1",
		Signer: signer, TrafficLimiter: limiter}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || limiter.bytes != 6 {
		t.Fatalf("replication limiter bytes=%d code=%d",
			limiter.bytes, rec.Code)
	}
}

type countingLimiter struct {
	bytes int64
}

func (l *countingLimiter) WrapWriter(_ context.Context, w io.Writer) io.Writer {
	return countingWriter{dst: w, limiter: l}
}

type countingWriter struct {
	dst     io.Writer
	limiter *countingLimiter
}

func (w countingWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.limiter.bytes += int64(n)
	return n, err
}

func testDownloadClaims() downloadtoken.Claims {
	return downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32",
		ExpiresAt:    time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes:     10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
}

func testReplicationClaims() downloadtoken.ReplicationClaims {
	return downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID: "req-1", TaskID: "task-1"}
}

func TestHandlerSwarmUsesGlobalAndChildLimiterWithoutPublicAccounting(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	token := swarmToken(t, signer, "asset-1", "manifest-1", "node-1", 6, swarm.MinPieceSize, 1)
	req := httptest.NewRequest(http.MethodGet, "/internal/swarm/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=0-5")
	rec := httptest.NewRecorder()
	global := &countingLimiter{}
	child := &countingLimiter{}
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer,
		TrafficLimiter: global, SwarmLimiter: child}).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || global.bytes != 6 || child.bytes != 6 {
		t.Fatalf("code=%d global=%d child=%d", rec.Code, global.bytes, child.bytes)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_traffic_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("internal swarm bytes entered public accounting: %d events", events)
	}
}
