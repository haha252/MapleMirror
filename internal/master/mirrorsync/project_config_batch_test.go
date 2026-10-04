package mirrorsync

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestProjectConfigSkipsUnchangedSQLAndRestoresResetScanState(t *testing.T) {
	db, closeDB := testMirrorSyncDB(t)
	defer closeDB()
	store := Store{DB: db}
	ctx := context.Background()
	projects := config.Projects{Projects: []config.Project{testProject("p1", "owner/repo", true)}}
	if err := store.SyncProjectConfig(ctx, projects); err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if err := store.SetProjectNextScan(ctx, "p1", next); err != nil {
		t.Fatal(err)
	}
	mustExecMirrorSync(t, db, `CREATE TRIGGER reject_project_upsert BEFORE INSERT ON projects
		BEGIN SELECT RAISE(ABORT,'unexpected unchanged project upsert'); END`)
	mustExecMirrorSync(t, db, `CREATE TRIGGER reject_scan_upsert BEFORE INSERT ON project_scan_state
		BEGIN SELECT RAISE(ABORT,'unexpected unchanged scan upsert'); END`)
	if err := store.SyncProjectConfig(ctx, projects); err != nil {
		t.Fatalf("unchanged config issued redundant SQL: %v", err)
	}
	mustExecMirrorSync(t, db, `DROP TRIGGER reject_scan_upsert`)
	mustExecMirrorSync(t, db, `DELETE FROM project_scan_state WHERE project_id='p1'`)
	if err := store.SyncProjectConfig(ctx, projects); err != nil {
		t.Fatalf("scan state reset was not observed: %v", err)
	}
	due, err := store.DueProjects(ctx, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil || len(due) != 1 || due[0] != "p1" {
		t.Fatalf("reset project not due: ids=%v err=%v", due, err)
	}
	// A display-name change is not part of the scan hash, but must still persist.
	projects.Projects[0].Name = "renamed"
	if err := store.SyncProjectConfig(ctx, projects); err == nil {
		t.Fatal("expected injected project write failure")
	}
	mustExecMirrorSync(t, db, `DROP TRIGGER reject_project_upsert`)
	if err := store.SyncProjectConfig(ctx, projects); err != nil {
		t.Fatalf("failed sync was not retried: %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM projects WHERE id='p1'`).Scan(&name); err != nil || name != "renamed" {
		t.Fatalf("name=%s err=%v", name, err)
	}
}
