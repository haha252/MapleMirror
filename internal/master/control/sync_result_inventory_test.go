package control

import (
	"context"
	"database/sql"
	"testing"

	"mirror-server/internal/protocol"
)

func TestSucceededSyncTaskResultStoresVerifiedInventory(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-ok", "asset-1", 0, "")
	markTaskRunning(t, repo, "task-ok")

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:            "task-ok",
		AssetID:           "asset-1",
		Result:            "succeeded",
		LocalDigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SizeBytes:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSyncTaskInventory(t, repo, "task-ok", "succeeded", "verified")
	var nodeState, certStatus string
	_ = repo.DB.QueryRow("SELECT state FROM nodes WHERE id = ?", session.NodeID).Scan(&nodeState)
	_ = repo.DB.QueryRow("SELECT status FROM node_certificates WHERE id = 'cert-1'").Scan(&certStatus)
	if nodeState == "disabled" || certStatus != "active" {
		t.Fatalf("valid succeeded result should not quarantine state=%s cert=%s",
			nodeState, certStatus)
	}
}

func TestSucceededSyncTaskResultMismatchQuarantinesPublicAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-bad", "asset-1", 0, "")
	markTaskRunning(t, repo, "task-bad")

	result, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:            "task-bad",
		AssetID:           "asset-1",
		Result:            "succeeded",
		LocalDigestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SizeBytes:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RoutingReady || result.ManagedState != "syncing" {
		t.Fatalf("quarantined mismatch should not be routing ready: %+v", result)
	}
	assertSyncTaskInventory(t, repo, "task-bad", "retry_wait", "mismatch")
	var nodeState, certStatus string
	var audits int
	_ = repo.DB.QueryRow("SELECT state FROM nodes WHERE id = ?", session.NodeID).Scan(&nodeState)
	_ = repo.DB.QueryRow("SELECT status FROM node_certificates WHERE id = 'cert-1'").Scan(&certStatus)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE operation = 'node.security_quarantine' AND target_id = ?`, session.NodeID).Scan(&audits)
	if nodeState != "disabled" || certStatus != "revoked" || audits != 1 {
		t.Fatalf("public mismatch should quarantine node state=%s cert=%s audits=%d",
			nodeState, certStatus, audits)
	}
	if repo.runtime().ActiveSession(session.NodeID) {
		t.Fatal("runtime session should be closed after sync result quarantine")
	}
}

func assertSyncTaskInventory(t *testing.T, repo Repository, taskID, taskState, inventoryState string) {
	t.Helper()
	var gotTaskState, gotInventoryState string
	var completed sql.NullString
	err := repo.DB.QueryRow(`SELECT state, completed_at FROM node_tasks WHERE id = ?`, taskID).
		Scan(&gotTaskState, &completed)
	if err != nil {
		t.Fatal(err)
	}
	if gotTaskState != taskState {
		t.Fatalf("task state=%s want=%s", gotTaskState, taskState)
	}
	if taskState != "succeeded" && completed.Valid && completed.String != "" {
		t.Fatalf("non-succeeded task should not have completed_at=%q", completed.String)
	}
	err = repo.DB.QueryRow(`SELECT state FROM node_inventory
		WHERE node_id = 'node-1' AND asset_id = 'asset-1'`).Scan(&gotInventoryState)
	if err != nil {
		t.Fatal(err)
	}
	if gotInventoryState != inventoryState {
		t.Fatalf("inventory state=%s want=%s", gotInventoryState, inventoryState)
	}
}
