package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterBackfillsAdminBlockDisplayIPForExistingV1Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 1, '` + now + `')`,
		`CREATE TABLE admin_ip_blocks (
			ip_key TEXT PRIMARY KEY,
			masked_ip TEXT NOT NULL,
			reason TEXT NOT NULL,
			blocked_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			attempts_after_block INTEGER NOT NULL DEFAULT 0,
			last_attempt_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
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
	assertColumn(t, opened, "admin_ip_blocks", "display_ip")
	assertDBVersion(t, opened, "master", 2)
}

func TestOpenNodeBackfillsInventoryForceColumnForExistingV1Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('node', 1, '` + now + `')`,
		`CREATE TABLE control_identity (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			node_id TEXT NOT NULL,
			certificate_fingerprint TEXT NOT NULL,
			certificate_not_after TEXT NOT NULL,
			enrolled_at TEXT NOT NULL,
			certificate_pem TEXT,
			ca_pem TEXT,
			private_key_pem TEXT,
			download_token_public_key_pem TEXT
		)`,
		`CREATE TABLE inventory_report_cursor (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			next_revision INTEGER NOT NULL,
			last_acked_revision INTEGER NOT NULL,
			updated_at TEXT NOT NULL
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

	opened, err := OpenNode(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	assertColumn(t, opened, "inventory_report_cursor", "force_report_requested_at")
	assertColumn(t, opened, "pending_sync_task_results", "peer_fallback_attempted")
	assertDBVersion(t, opened, "node", 3)
}

func TestMissingUpgradeDoesNotAdvanceDatabaseVersion(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE database_version (
		kind TEXT PRIMARY KEY,
		version INTEGER NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := setVersion(tx, "test", 1); err != nil {
		t.Fatal(err)
	}
	_, err = applyVersionPlan(context.Background(), tx, databaseVersionPlan{
		Kind: "test", CurrentVersion: 2,
	}, 1)
	if err == nil {
		t.Fatal("expected missing upgrade to fail")
	}
	var got int
	if err := tx.QueryRow(`SELECT version FROM database_version WHERE kind = 'test'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("version advanced after missing upgrade: %d", got)
	}
}

func TestFutureDatabaseVersionFailsWithoutChangingVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('node', 4, '` + now + `')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenNode(path)
	if err == nil {
		_ = opened.Close()
		t.Fatal("expected unsupported future version to fail")
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	assertDBVersion(t, check, "node", 4)
}
