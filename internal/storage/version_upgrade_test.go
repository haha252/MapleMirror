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
	assertDBVersion(t, opened, "master", 9)
}

func TestOpenMasterBackfillsDownloadPriorityForExistingV3Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 3, '` + now + `')`,
		`CREATE TABLE nodes (
			id TEXT PRIMARY KEY,
			public_name TEXT NOT NULL,
			certificate_fingerprint TEXT,
			state TEXT NOT NULL,
			target_bandwidth_bps INTEGER NOT NULL,
			last_heartbeat_at TEXT,
			routing_ready INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`INSERT INTO nodes
			(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
			VALUES ('node-1', '节点一', 'online', 0, 0, '` + now + `', '` + now + `')`,
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
	assertColumn(t, opened, "nodes", "download_priority")
	assertDBVersion(t, opened, "master", 9)
	var priority int
	if err := opened.QueryRow(`SELECT download_priority FROM nodes WHERE id = 'node-1'`).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	if priority != 50 {
		t.Fatalf("download_priority=%d want 50", priority)
	}
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
	assertTable(t, opened, "local_authorizations")
	assertDBVersion(t, opened, "node", 5)
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
	var events []versionLogEvent
	_, err = applyVersionPlan(context.Background(), tx, databaseVersionPlan{
		Kind: "test", CurrentVersion: 2,
	}, 1, collectVersionLogEvents(&events))
	if err == nil {
		t.Fatal("expected missing upgrade to fail")
	}
	if len(events) != 0 {
		t.Fatalf("expected no version upgrade logs, got %+v", events)
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
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('node', 6, '` + now + `')`,
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
	assertDBVersion(t, check, "node", 6)
}
