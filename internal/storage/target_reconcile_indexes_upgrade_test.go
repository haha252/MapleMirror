package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestMasterV15UpgradeKeepsManualTargetInventoryIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 14, '` + now + `')`,
		`CREATE TABLE target_inventory (
			node_id TEXT NOT NULL,
			asset_id TEXT NOT NULL,
			desired_state TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (node_id, asset_id)
		)`,
		`CREATE TABLE node_project_assignments (
			node_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			mode TEXT NOT NULL,
			assigned INTEGER NOT NULL,
			score INTEGER NOT NULL DEFAULT 0,
			pinned INTEGER NOT NULL DEFAULT 0,
			last_changed_at TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (node_id, project_id)
		)`,
		`CREATE INDEX idx_target_inventory_asset_state_node
			ON target_inventory(asset_id, desired_state, node_id)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	wal := false
	opened, err := OpenMaster(config.Database{
		Path: path, BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	assertIndex(t, opened, "idx_target_inventory_asset_state_node")
	assertIndex(t, opened, "idx_node_project_assignments_project_assigned_node")
	assertDBVersion(t, opened, "master", masterDBVersion)
}
