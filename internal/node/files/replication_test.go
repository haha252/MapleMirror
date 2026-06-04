package files

import (
	"net/http"
	"net/http/httptest"
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
