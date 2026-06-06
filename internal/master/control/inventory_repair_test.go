package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestCompleteInventoryReportCreatesRepairTaskForMissingTarget(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.DB.Exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		10, 'old', 'verified')`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-missing", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	err = repo.DB.QueryRow(`SELECT state FROM node_inventory
		WHERE node_id = ? AND asset_id = 'asset-1'`, session.NodeID).Scan(&state)
	if err != nil || state != "missing" {
		t.Fatalf("missing inventory state got=%q err=%v", state, err)
	}
	var tasks int
	err = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = 'asset-1' AND state = 'pending'`,
		session.NodeID).Scan(&tasks)
	if err != nil || tasks != 1 {
		t.Fatalf("repair task count=%d err=%v", tasks, err)
	}
}

func TestChunkedInventoryReportKeepsEarlierChunkVerified(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedSecondAssetTarget(t, repo, session.NodeID)
	firstReportedAt := time.Now().UTC().Add(-time.Minute)
	finalReportedAt := time.Now().UTC()
	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-chunk-1", Revision: 1, GeneratedAt: firstReportedAt, Complete: false,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-chunk-2", Revision: 1, GeneratedAt: finalReportedAt, Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-2", SizeBytes: 20,
			DigestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing, tasks int
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_inventory
		WHERE node_id = ? AND state != 'verified'`, session.NodeID).Scan(&missing)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks WHERE node_id = ?`, session.NodeID).Scan(&tasks)
	if missing != 0 || tasks != 0 {
		t.Fatalf("chunked inventory should keep both assets verified, missing=%d tasks=%d", missing, tasks)
	}
}

func TestRepairTaskResetsFailedDownloadTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, attempts, error_message)
		VALUES ('task-failed', ?, 'asset_download', 'asset-1', 'failed',
		'req-1', 'old', 'old', 4, '失败')`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-missing", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	err = repo.DB.QueryRow(`SELECT state, attempts FROM node_tasks
		WHERE id = 'task-failed'`).Scan(&state, &attempts)
	if err != nil || state != "pending" || attempts != 0 {
		t.Fatalf("failed task should reset to pending, state=%s attempts=%d err=%v", state, attempts, err)
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
