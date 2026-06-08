package mirrorsync

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestSyncProjectConfigObsoletesDisabledProjectTasks(t *testing.T) {
	db, store := seedProjectConfigTask(t)
	defer db.Close()

	disabled := config.Projects{Projects: []config.Project{testProject("p1", "owner/repo", false)}}
	if err := store.SyncProjectConfig(context.Background(), disabled); err != nil {
		t.Fatal(err)
	}

	assertProjectEnabled(t, db, "p1", false)
	assertTargetState(t, db, "remove")
	assertWhereCount(t, db, "node_tasks", "state = 'obsolete'", 4)
}

func TestSyncProjectConfigObsoletesRemovedProjectTasks(t *testing.T) {
	db, store := seedProjectConfigTask(t)
	defer db.Close()

	if err := store.SyncProjectConfig(context.Background(), config.Projects{}); err != nil {
		t.Fatal(err)
	}

	assertProjectEnabled(t, db, "p1", false)
	assertTargetState(t, db, "remove")
	assertWhereCount(t, db, "node_tasks", "state = 'obsolete'", 4)
}

func seedProjectConfigTask(t *testing.T) (*sql.DB, Store) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedNode(t, db)
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()}}
	projects := config.Projects{Projects: []config.Project{testProject("p1", "owner/repo", true)}}
	if _, err := scanner.Scan(context.Background(), projects, "", "req-seed"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at, retry_after)
		VALUES ('task-retry', 'node-1', 'asset_download', 'p1:1:1',
		'retry_wait', 'req-retry', 'now', 'now', '2000-01-01T00:00:00Z')`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES
		('task-sent', 'node-1', 'asset_download', 'p1:1:1', 'sent', 'req-sent', 'now', 'now'),
		('task-running', 'node-1', 'asset_download', 'p1:1:1', 'running', 'req-running', 'now', 'now')`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db, Store{DB: db}
}
