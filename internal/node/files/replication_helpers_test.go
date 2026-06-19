package files

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func serveReplicationAsset(t *testing.T, db *sql.DB, storageDir string,
	signer downloadtoken.Signer) *httptest.ResponseRecorder {
	t.Helper()
	token := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID:      "asset-1",
		SourceNodeID: "node-1",
		TargetNodeID: "node-2",
		ExpiresAt:    time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		RequestID:    "req-1",
		TaskID:       "task-1",
	})
	req := httptest.NewRequest(http.MethodGet, "/internal/replication/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	return rec
}
