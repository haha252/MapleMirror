package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterCreatesHistoricalNodeTrafficTablesForExistingV21Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`CREATE TABLE database_version (
		kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL
	);
	INSERT INTO database_version(kind, version, updated_at)
	VALUES ('master', 21, ?);`, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	assertTable(t, opened, "historical_node_traffic_totals")
	assertTable(t, opened, "historical_daily_node_traffic_stats")
	assertIndex(t, opened, "idx_historical_daily_node_traffic_day")
	assertDBVersion(t, opened, "master", masterDBVersion)
}
