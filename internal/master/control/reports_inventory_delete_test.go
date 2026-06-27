package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestCompleteInventoryReportRemovesVerifiedNonTargetAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)

	result, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-remove-non-target", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID:      "asset-new",
			DigestSHA256: "sha256:new",
			SizeBytes:    20,
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var desiredState string
	if err := repo.DB.QueryRow(`SELECT desired_state FROM target_inventory
		WHERE node_id = ? AND asset_id = 'asset-new'`, session.NodeID).Scan(&desiredState); err != nil {
		t.Fatal(err)
	}
	if desiredState != "remove" {
		t.Fatalf("desired_state=%s want remove", desiredState)
	}
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete' AND state = 'pending'", 1)
	if !result.SyncTasksChanged {
		t.Fatal("non-target verified asset should trigger delete tasks changed")
	}
}

func TestCompleteInventoryReportCleansHistoricalVerifiedResidue(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-new', 'sha256:new', 20, 'old', 'verified')`, session.NodeID)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-clean-residue", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND desired_state = 'remove'", 1)
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete' AND state = 'pending'", 1)
}

func TestCompleteInventoryReportDoesNotDuplicateSucceededDeleteTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-new', 'sha256:new', 20, 'old', 'verified')`, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at, completed_at)
		VALUES ('delete-done', ?, 'asset_delete', 'asset-new', 'succeeded',
		'req-done', 'old', 'old', 'old')`, session.NodeID)

	result, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-no-dup-delete", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete'", 1)
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete' AND state = 'pending'", 0)
	if result.SyncTasksChanged {
		t.Fatal("existing succeeded delete task should not count as new task change")
	}
}

func TestCompleteInventoryReportRemovedNonTargetAssetDoesNotCreateDeleteTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-new', 'sha256:new', 20, 'old', 'verified')`, session.NodeID)

	result, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-removed-non-target", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID:      "asset-new",
			DigestSHA256: "sha256:new",
			SizeBytes:    20,
			LocalState:   "removed",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var state, digest string
	var size int64
	if err := repo.DB.QueryRow(`SELECT state, local_digest_sha256, size_bytes
		FROM node_inventory WHERE node_id = ? AND asset_id = 'asset-new'`,
		session.NodeID).Scan(&state, &digest, &size); err != nil {
		t.Fatal(err)
	}
	if state != "removed" || digest != "" || size != 0 {
		t.Fatalf("removed non-target inventory got state=%s digest=%q size=%d",
			state, digest, size)
	}
	var targetCount int
	var desiredState string
	if err := repo.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(desired_state), '')
		FROM target_inventory WHERE node_id = ? AND asset_id = 'asset-new'`,
		session.NodeID).Scan(&targetCount, &desiredState); err != nil {
		t.Fatal(err)
	}
	if targetCount != 0 {
		t.Fatalf("unexpected target_inventory for removed non-target asset count=%d desired_state=%q",
			targetCount, desiredState)
	}
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete'", 0)
	if result.SyncTasksChanged {
		t.Fatal("removed non-target asset should not create delete task")
	}
}

func TestCompleteInventoryReportKeepsRequiredAssetRequired(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-required-stays-required", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND desired_state = 'required'", 1)
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND desired_state = 'remove'", 0)
}
