package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestTrafficEventAccumulatesDownloadHistoryOnce(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()
	seedTrafficAuth(t, repo)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := repo.DB.Exec(`INSERT INTO download_history
		(authorization_id, client_prefix_key, source_kind, project_id, project_name,
		asset_id, file_name, version, system, architecture, node_id, node_name,
		issued_at, expires_at, status, request_id, updated_at)
		VALUES ('auth-1', '192.0.2.1/32', 'web', 'p1', '项目一', 'asset-1',
		'a.zip', 'v1', '', 'amd64', 'node-1', '节点一', ?, ?, 'issued',
		'master-req-1', ?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	session := Session{ID: "sess-1", NodeID: "node-1"}
	event := protocol.TrafficEvent{
		EventSequence: 1, AuthorizationID: "auth-1", AssetID: "asset-1",
		NodeRequestID: "node-req-1", MasterRequestID: "master-req-1",
		SentBytes: 5, Status: "completed", ReportedAt: time.Now().UTC(),
	}
	if _, err := repo.AcceptTrafficEvent(context.Background(), session, 2, event); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptTrafficEvent(context.Background(), session, 3, event); err != nil {
		t.Fatal(err)
	}
	var sent int64
	var status, first, last string
	err := repo.DB.QueryRow(`SELECT sent_bytes, status, first_transfer_at, last_transfer_at
		FROM download_history WHERE authorization_id = 'auth-1'`).
		Scan(&sent, &status, &first, &last)
	if err != nil || sent != 5 || status != "active" || first == "" || last == "" {
		t.Fatalf("history sent=%d status=%q first=%q last=%q err=%v",
			sent, status, first, last, err)
	}
}
