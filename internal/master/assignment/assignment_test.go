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
