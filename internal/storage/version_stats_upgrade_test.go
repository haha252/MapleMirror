package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterCreatesPublicStatsIndexesForExistingV6Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 6, '` + now + `')`,
		`CREATE TABLE nodes (
			id TEXT PRIMARY KEY,
			public_name TEXT NOT NULL,
			state TEXT NOT NULL,
			target_bandwidth_bps INTEGER NOT NULL,
			routing_ready INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE node_availability_samples (
			node_id TEXT NOT NULL REFERENCES nodes(id),
			sample_start TEXT NOT NULL,
			sample_end TEXT NOT NULL,
			routable INTEGER NOT NULL,
			heartbeat_ok INTEGER NOT NULL,
			PRIMARY KEY (node_id, sample_start)
		)`,
		`CREATE TABLE daily_node_traffic_stats (
			stat_day TEXT NOT NULL,
			node_id TEXT NOT NULL REFERENCES nodes(id),
			sent_bytes INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (stat_day, node_id)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	assertIndex(t, opened, "idx_node_availability_samples_window")
	assertIndex(t, opened, "idx_daily_node_traffic_stats_node")
	assertDBVersion(t, opened, "master", 9)
}
