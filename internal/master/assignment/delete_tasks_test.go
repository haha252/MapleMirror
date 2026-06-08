package assignment

import (
	"context"
	"database/sql"
	"testing"
)

func TestReconcileGeneratesDeleteTaskForRemovedTarget(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	seedProject(t, db, "p1", "项目一", 10)
	reconcile(t, db)
	generateNodeTasks(t, db, "node-1", "now")
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-p1', 'sha256', 64, 'now', 'verified')`)
	mustExec(t, db, `UPDATE node_project_assignments SET assigned = 0
		WHERE node_id = 'node-1' AND project_id = 'p1'`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := rebuildNodeTargets(context.Background(), tx, "node-1", "later"); err != nil {
		t.Fatal(err)
	}
	if err := CancelObsoleteNodeTasks(context.Background(), tx, "node-1", "later"); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateNodeTasks(context.Background(), tx, "node-1", "later"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertTaskCount(t, db, "asset_delete", "pending", 1)
	assertTaskCount(t, db, "asset_download", "obsolete", 1)
}

func TestReconcileCancelsDeleteTaskWhenTargetRequiredAgain(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	seedProject(t, db, "p1", "项目一", 10)
	reconcile(t, db)
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-p1', 'sha256', 64, 'now', 'verified')`)
	mustExec(t, db, `UPDATE target_inventory SET desired_state = 'remove'
		WHERE node_id = 'node-1' AND asset_id = 'asset-p1'`)
	generateNodeTasks(t, db, "node-1", "remove-time")
	assertTaskCount(t, db, "asset_delete", "pending", 1)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := rebuildNodeTargets(context.Background(), tx, "node-1", "required-time"); err != nil {
		t.Fatal(err)
	}
	if err := CancelObsoleteNodeTasks(context.Background(), tx, "node-1", "required-time"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertTaskCount(t, db, "asset_delete", "obsolete", 1)
}

func generateNodeTasks(t *testing.T, db *sql.DB, nodeID, now string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateNodeTasks(context.Background(), tx, nodeID, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
