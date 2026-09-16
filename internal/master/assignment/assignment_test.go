package assignment

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestAutoAssignmentUsesTopProjects(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 2)
	seedProject(t, db, "p1", "项目一", 30)
	seedProject(t, db, "p2", "项目二", 90)
	seedProject(t, db, "p3", "项目三", 60)
	reconcile(t, db)

	assertAssigned(t, db, "p1", false)
	assertAssigned(t, db, "p2", true)
	assertAssigned(t, db, "p3", true)
	assertRequiredTargets(t, db, 2)
}

func TestReconcileAllNodesIncludesDisabledNodes(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	mustExec(t, db, `UPDATE nodes SET state = 'disabled' WHERE id = 'node-1'`)
	seedProject(t, db, "p1", "项目一", 30)

	reconcile(t, db)

	assertAssigned(t, db, "p1", true)
	assertRequiredTargets(t, db, 1)
}

func TestAutoAssignmentKeepsCloseProject(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 2)
	seedProject(t, db, "p1", "项目一", 200)
	seedProject(t, db, "p2", "项目二", 100)
	seedProject(t, db, "p3", "项目三", 110)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-1', 'p1', 'auto', 1, 200, 0, '2026-06-06T12:00:00Z', 'now')`)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-1', 'p2', 'auto', 1, 100, 0, '2026-06-01T00:00:00Z', 'now')`)
	reconcile(t, db)

	assertAssigned(t, db, "p2", true)
	assertAssigned(t, db, "p3", false)
}

func TestAutoAssignmentKeepsRecentProject(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 2)
	seedProject(t, db, "p1", "项目一", 200)
	seedProject(t, db, "p2", "项目二", 10)
	seedProject(t, db, "p3", "项目三", 1000)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-1', 'p1', 'auto', 1, 200, 0, '2026-06-06T12:00:00Z', 'now')`)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-1', 'p2', 'auto', 1, 10, 0, '2026-06-06T12:00:00Z', 'now')`)
	reconcile(t, db)

	assertAssigned(t, db, "p2", true)
	assertAssigned(t, db, "p3", false)
}

func TestReconcileNodeDoesNotRefreshUnchangedAssignmentTargets(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	seedProject(t, db, "p1", "项目一", 30)
	reconcile(t, db)
	mustExec(t, db, `UPDATE node_project_assignments SET updated_at = 'stable'
		WHERE node_id = 'node-1' AND project_id = 'p1'`)
	mustExec(t, db, `UPDATE target_inventory SET updated_at = 'stable'
		WHERE node_id = 'node-1' AND asset_id = 'asset-p1'`)

	reconcile(t, db)

	assertTimestamp(t, db, "node_project_assignments", "stable",
		"node_id = 'node-1' AND project_id = 'p1'")
	assertTimestamp(t, db, "target_inventory", "stable",
		"node_id = 'node-1' AND asset_id = 'asset-p1'")
}

func TestRebuildProjectTargetsMarksOnlyProjectTargets(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	mustExec(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready,
		max_mirror_projects, created_at, updated_at)
		VALUES ('node-2', '节点二', 'online', 0, 0, 1, 'now', 'now')`)
	seedProject(t, db, "p1", "项目一", 30)
	seedProject(t, db, "p2", "项目二", 20)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-1', 'p1', 'auto', 1, 30, 0, 'old', 'old')`)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-2', 'p1', 'auto', 0, 30, 0, 'old', 'old')`)
	mustExec(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-p1', 'required', 'stable'),
			('node-2', 'asset-p1', 'required', 'old'),
			('node-2', 'asset-p2', 'required', 'stable')`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := RebuildProjectTargets(context.Background(), tx, "p1", "later"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertTargetState(t, db, "node-1", "asset-p1", "required", "stable")
	assertTargetState(t, db, "node-2", "asset-p1", "remove", "later")
	assertTargetState(t, db, "node-2", "asset-p2", "required", "stable")
}

func TestManualAssignmentRejectsOverLimit(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	seedProject(t, db, "p1", "项目一", 1)
	seedProject(t, db, "p2", "项目二", 2)
	err := SaveNodeProjects(context.Background(), db, "node-1", ModeManual,
		[]string{"p1", "p2"}, time.Now())
	if !errors.Is(err, ErrManualLimitExceeded) {
		t.Fatalf("err=%v, want ErrManualLimitExceeded", err)
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.OpenMaster(config.Database{
		Path:        filepath.Join(t.TempDir(), "master.db"),
		BusyTimeout: "5s", WAL: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedAssignmentNode(t *testing.T, db *sql.DB, max int) {
	t.Helper()
	mustExec(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready,
		max_mirror_projects, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, ?, 'now', 'now')`, max)
}

func TestRebuildProjectTargetsRetiresOldReleasePerNodeAfterReplacementVerified(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 1)
	mustExec(t, db, `UPDATE nodes SET state='online' WHERE id='node-1'`)
	mustExec(t, db, `INSERT INTO projects
		(id,name,repository,enabled,retain_versions,include_prerelease,download_multiplier,config_hash,updated_at)
		VALUES ('rollout','Rollout','owner/repo',1,1,0,1,'hash','now')`)
	mustExec(t, db, `INSERT INTO releases
		(id,project_id,github_release_id,tag_name,prerelease,published_at,selected,created_at)
		VALUES ('old','rollout',1,'v1',0,'2026-01-01T00:00:00Z',0,'now'),
		       ('new','rollout',2,'v2',0,'2026-02-01T00:00:00Z',1,'now')`)
	mustExec(t, db, `INSERT INTO assets
		(id,release_id,github_asset_id,file_name,architecture,size_bytes,source_url,digest_sha256,service_state,created_at)
		VALUES ('asset-old','old',1,'app.zip','',10,'https://example/old','sha256:old','candidate','now'),
		       ('asset-new','new',2,'app.zip','',20,'https://example/new','sha256:new','candidate','now')`)
	mustExec(t, db, `INSERT INTO node_project_assignments
		(node_id,project_id,mode,assigned,score,pinned,last_changed_at,updated_at)
		VALUES ('node-1','rollout','auto',1,1,0,'old','old')`)
	mustExec(t, db, `INSERT INTO target_inventory(node_id,asset_id,desired_state,updated_at)
		VALUES ('node-1','asset-old','required','old')`)
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id,asset_id,local_digest_sha256,size_bytes,verified_at,state)
		VALUES ('node-1','asset-old','sha256:old',10,'now','verified')`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := RebuildProjectTargets(context.Background(), tx, "rollout", "first"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertTargetState(t, db, "node-1", "asset-old", "required", "old")
	assertTargetState(t, db, "node-1", "asset-new", "required", "first")

	mustExec(t, db, `INSERT INTO node_inventory
		(node_id,asset_id,local_digest_sha256,size_bytes,verified_at,state)
		VALUES ('node-1','asset-new','sha256:new',20,'now','verified')`)
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := RebuildProjectTargets(context.Background(), tx, "rollout", "second"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertTargetState(t, db, "node-1", "asset-old", "remove", "second")
}
