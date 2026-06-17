package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestCompleteInventoryReportPreservesLeasedMissingDownloadTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	lease := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, lease_expires_at)
		VALUES ('task-running', ?, 'asset_download', 'asset-1', 'running',
		'req-1', 'old', 'old', ?)`, session.NodeID, lease)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-missing-running", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, leaseAfter string
	err = repo.DB.QueryRow(`SELECT state, COALESCE(lease_expires_at, '')
		FROM node_tasks WHERE id = 'task-running'`).Scan(&state, &leaseAfter)
	if err != nil || state != "running" || leaseAfter != lease {
		t.Fatalf("missing complete inventory should preserve leased running task, state=%s lease=%q err=%v",
			state, leaseAfter, err)
	}
	_, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || ok {
		t.Fatalf("leased task should not redispatch immediately, ok=%v err=%v", ok, err)
	}
}

func TestCompleteInventoryReportPreservesFutureRetryWaitDownloadTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	retryAfter := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, attempts, error_message, retry_after)
		VALUES ('task-retry-wait', ?, 'asset_download', 'asset-1', 'retry_wait',
		'req-1', 'old', 'old', 2, 'temporary error', ?)`, session.NodeID, retryAfter)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-missing-retry-wait", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, retryAfterAfter string
	var attempts int
	err = repo.DB.QueryRow(`SELECT state, attempts, COALESCE(retry_after, '')
		FROM node_tasks WHERE id = 'task-retry-wait'`).Scan(&state, &attempts, &retryAfterAfter)
	if err != nil || state != "retry_wait" || attempts != 2 || retryAfterAfter != retryAfter {
		t.Fatalf("missing complete inventory should preserve future retry wait, state=%s attempts=%d retry_after=%q err=%v",
			state, attempts, retryAfterAfter, err)
	}
	_, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || ok {
		t.Fatalf("future retry_wait task should not redispatch immediately, ok=%v err=%v", ok, err)
	}
}

func TestCompleteInventoryReportResetsExpiredRetryWaitDownloadTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	retryAfter := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, attempts, error_message, retry_after)
		VALUES ('task-retry-expired', ?, 'asset_download', 'asset-1', 'retry_wait',
		'req-1', 'old', 'old', 2, 'temporary error', ?)`, session.NodeID, retryAfter)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-missing-retry-expired", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, retryAfterAfter string
	err = repo.DB.QueryRow(`SELECT state, COALESCE(retry_after, '')
		FROM node_tasks WHERE id = 'task-retry-expired'`).Scan(&state, &retryAfterAfter)
	if err != nil || state != "pending" || retryAfterAfter != "" {
		t.Fatalf("expired retry_wait task should reset to pending, state=%s retry_after=%q err=%v",
			state, retryAfterAfter, err)
	}
}

func TestCompleteInventoryReportCancelsSatisfiedPendingTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-stale', ?, 'asset_download', 'asset-1', 'pending',
		'req-1', 'old', 'old')`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-ok", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	err = repo.DB.QueryRow(`SELECT state FROM node_tasks WHERE id = 'task-stale'`).Scan(&state)
	if err != nil || state != "cancelled" {
		t.Fatalf("satisfied task should be cancelled, state=%s err=%v", state, err)
	}
	var ready int
	_ = repo.DB.QueryRow(`SELECT routing_ready FROM nodes WHERE id = ?`, session.NodeID).Scan(&ready)
	if ready != 1 {
		t.Fatalf("verified inventory should restore ready, got %d", ready)
	}
}

func seedSecondAssetTarget(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	_, err := repo.DB.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-2', 'rel-1', 2, 'app2.zip', 'amd64', 20, 'https://example.invalid',
		'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		'candidate', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, 'asset-2', 'required', 'now')`, nodeID)
	if err != nil {
		t.Fatal(err)
	}
}
