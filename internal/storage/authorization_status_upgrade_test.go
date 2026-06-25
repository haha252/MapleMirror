package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterBackfillsAuthorizationStatusForExistingV4Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 4, '` + now + `')`,
		`CREATE TABLE download_authorizations (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			client_prefix_key TEXT NOT NULL,
			issued_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			max_bytes INTEGER NOT NULL,
			range_limit INTEGER NOT NULL,
			status TEXT NOT NULL,
			request_id TEXT NOT NULL,
			first_transfer_at TEXT
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
	assertColumn(t, opened, "download_authorizations", "status_reason")
	assertColumn(t, opened, "download_authorizations", "status_updated_at")
	assertColumn(t, opened, "download_authorizations", "token_hash")
	assertDBVersion(t, opened, "master", 6)
}
