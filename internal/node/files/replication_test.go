package files

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestHandlerServesReplicationTokenAsset(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	token := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID: "req-1", TaskID: "task-1",
	})
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "abcdef" {
		t.Fatalf("replication response mismatch code=%d body=%q", rec.Code, rec.Body.String())
	}
	var events int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pending_traffic_events`).Scan(&events)
	if events != 0 {
		t.Fatalf("replication should not record public traffic events, got %d", events)
	}
}

func TestHandlerRejectsReusedReplicationToken(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	token := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID: "req-1", TaskID: "task-1",
	})
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first replication request should succeed: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused replication token should be rejected: %d", rec.Code)
	}
}

func TestHandlerRejectsReplicationQueryTokenAndHead(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	token := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID: "req-1", TaskID: "task-1",
	})
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1?token="+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replication query token should be rejected: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodHead, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("replication HEAD should be rejected: %d", rec.Code)
	}
}

func TestHandlerRejectsModifiedReplicationAsset(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	fresh := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, fresh)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.WriteFile(path, []byte("zzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	token := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID: "req-1", TaskID: "task-1",
	})
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("验证后改写的复制资产应被拒绝：%d", rec.Code)
	}
	assertLocalAssetState(t, db, "mismatch")
}

func TestHandlerRejectsBackdatedModifiedReplicationAsset(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	freshAt := time.Now().Add(-10 * time.Second).UTC()
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, freshAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	tamperAssetWithBackdatedMTime(t, storageDir, freshAt)
	token := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID: "req-1", TaskID: "task-1",
	})
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("backdated modified replication asset should be rejected: %d", rec.Code)
	}
	assertLocalAssetState(t, db, "mismatch")
}

func TestHandlerRejectsReplicationWithoutToken(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing replication token should be rejected: %d", rec.Code)
	}
}

func TestHandlerRejectsReplicationTokenFailures(t *testing.T) {
	tests := []struct {
		name   string
		claims downloadtoken.ReplicationClaims
		state  string
	}{
		{name: "expired", claims: downloadtoken.ReplicationClaims{
			AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
			ExpiresAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		}, state: "verified"},
		{name: "cross source", claims: downloadtoken.ReplicationClaims{
			AssetID: "asset-1", SourceNodeID: "other", TargetNodeID: "node-2",
			ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		}, state: "verified"},
		{name: "cross asset", claims: downloadtoken.ReplicationClaims{
			AssetID: "other", SourceNodeID: "node-1", TargetNodeID: "node-2",
			ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		}, state: "verified"},
		{name: "not verified", claims: downloadtoken.ReplicationClaims{
			AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2",
			ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		}, state: "missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, storageDir, signer := prepareNodeFile(t)
			_, _ = db.Exec(`UPDATE local_assets SET state = ? WHERE asset_id = 'asset-1'`, tt.state)
			token := signReplicationToken(t, signer, tt.claims)
			req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Fatalf("invalid replication request should be rejected")
			}
		})
	}
}

func signReplicationToken(t *testing.T, signer downloadtoken.Signer, claims downloadtoken.ReplicationClaims) string {
	t.Helper()
	token, err := signer.SignReplication(claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
