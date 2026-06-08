package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestInventoryReconcileResultWaitsForCompleteInventoryReport(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		10, 'old', 'verified')`, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, lease_expires_at)
		VALUES ('task-reconcile', ?, 'inventory_reconcile', NULL, 'running',
		'req-1', 'old', 'old', ?)`,
		session.NodeID, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))

	result, err := repo.AcceptSyncTaskResult(context.Background(), session, 2, protocol.SyncTaskResult{
		TaskID: "task-reconcile", Result: "succeeded", Message: "库存对账任务已确认",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RoutingReady || result.ManagedState != "syncing" {
		t.Fatalf("reconcile result alone must not restore ready: %+v", result)
	}
	var state, completed string
	err = repo.DB.QueryRow(`SELECT state, COALESCE(completed_at, '') FROM node_tasks
		WHERE id = 'task-reconcile'`).Scan(&state, &completed)
	if err != nil {
		t.Fatal(err)
	}
	if state != "running" || completed != "" {
		t.Fatalf("reconcile task should wait for inventory, state=%s completed=%q", state, completed)
	}
	var ready int
	_ = repo.DB.QueryRow(`SELECT routing_ready FROM nodes WHERE id = ?`, session.NodeID).Scan(&ready)
	if ready != 0 {
		t.Fatalf("node should stay not ready before inventory, ready=%d", ready)
	}
}

func TestCompleteInventoryReportCompletesInventoryReconcileTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, lease_expires_at)
		VALUES ('task-reconcile', ?, 'inventory_reconcile', NULL, 'running',
		'req-1', 'old', 'old', ?)`,
		session.NodeID, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
	if _, err := repo.AcceptSyncTaskResult(context.Background(), session, 2, protocol.SyncTaskResult{
		TaskID: "task-reconcile", Result: "succeeded", Message: "库存对账任务已确认",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := repo.AcceptInventoryReport(context.Background(), session, 3, protocol.InventoryReport{
		ReportID: "r-reconcile", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RoutingReady || result.ManagedState != "ready" {
		t.Fatalf("complete inventory should restore ready: %+v", result)
	}
	var state string
	var completed string
	err = repo.DB.QueryRow(`SELECT state, COALESCE(completed_at, '') FROM node_tasks
		WHERE id = 'task-reconcile'`).Scan(&state, &completed)
	if err != nil {
		t.Fatal(err)
	}
	if state != "succeeded" || completed == "" {
		t.Fatalf("reconcile task should complete after inventory, state=%s completed=%q", state, completed)
	}
}
