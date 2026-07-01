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

func TestOpenMasterCreatesInitialContractAndIsIdempotent(t *testing.T) {
	wal := true
	cfg := config.Database{Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal}
	db, err := OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertTable(t, db, "download_authorizations")
	assertTable(t, db, "traffic_events")
	assertTable(t, db, "traffic_event_dedupe")
	assertTable(t, db, "node_traffic_cursors")
	assertTable(t, db, "archive_migrations")
	assertTable(t, db, "node_enrollment_requests")
	assertTable(t, db, "node_control_sessions")
	assertTable(t, db, "sync_scans")
	assertTable(t, db, "project_scan_state")
	assertTable(t, db, "client_blocks")
	assertTable(t, db, "admin_web_sessions")
	assertTable(t, db, "admin_ip_blocks")
	assertColumn(t, db, "admin_ip_blocks", "display_ip")
	assertTable(t, db, "node_project_assignments")
	assertColumn(t, db, "node_tasks", "lease_expires_at")
	assertColumn(t, db, "nodes", "max_mirror_projects")
	assertColumn(t, db, "nodes", "project_assignment_mode")
	assertColumn(t, db, "nodes", "last_public_probe_at")
	assertColumn(t, db, "nodes", "public_probe_network_failures")
	assertColumn(t, db, "nodes", "download_priority")
	assertColumnDefault(t, db, "nodes", "download_priority", "50")
	assertColumn(t, db, "assets", "variant")
	assertColumn(t, db, "assets", "classification_reason")
	assertColumn(t, db, "download_authorizations", "token_hash")
	assertColumn(t, db, "download_authorizations", "source_kind")
	assertColumn(t, db, "daily_project_stats", "web_authorization_count")
	assertColumn(t, db, "daily_project_stats", "api_authorization_count")
	assertColumn(t, db, "daily_asset_stats", "web_authorization_count")
	assertColumn(t, db, "daily_asset_stats", "api_authorization_count")
	assertTable(t, db, "public_stat_totals")
	assertTable(t, db, "daily_public_stats")
	assertTable(t, db, "asset_stat_totals")
	assertTable(t, db, "project_stat_totals")
	assertTable(t, db, "node_traffic_totals")
	assertTable(t, db, "node_availability_rollups")
	assertIndex(t, db, "idx_node_availability_samples_window")
	assertIndex(t, db, "idx_node_availability_rollups_window")
	assertIndex(t, db, "idx_daily_node_traffic_stats_node")
	assertIndex(t, db, "idx_traffic_event_dedupe_authorization")
	assertIndex(t, db, "idx_asset_stat_totals_downloads")
	assertIndex(t, db, "idx_project_stat_totals_downloads")
	assertIndex(t, db, "idx_traffic_event_dedupe_accounted")
	assertIndex(t, db, "idx_traffic_event_dedupe_authorization_accounted")
	assertIndex(t, db, "idx_target_inventory_asset_state_node")
	assertIndex(t, db, "idx_node_project_assignments_project_assigned_node")
	_ = db.Close()
	db, err = OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertDBVersion(t, db, "master", masterDBVersion)
	var legacyCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 0 {
		t.Fatal("新数据库不应创建旧 schema_migrations 表")
	}
}

func TestOpenNodeCreatesPendingTrafficStore(t *testing.T) {
	db, err := OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertTable(t, db, "local_assets")
	assertTable(t, db, "pending_traffic_events")
	assertTable(t, db, "control_identity")
	assertTable(t, db, "local_authorizations")
	assertTable(t, db, "node_enrollment_state")
	assertTable(t, db, "local_sync_tasks")
	assertTable(t, db, "pending_sync_task_results")
	assertColumn(t, db, "pending_sync_task_results", "peer_fallback_attempted")
	assertColumn(t, db, "control_identity", "certificate_pem")
	assertColumn(t, db, "control_identity", "ca_pem")
	assertColumn(t, db, "control_identity", "private_key_pem")
	assertColumn(t, db, "control_identity", "download_token_public_key_pem")
	assertColumn(t, db, "inventory_report_cursor", "force_report_requested_at")
	assertColumn(t, db, "local_authorizations", "token_hash")
	assertDBVersion(t, db, "node", 5)
}

func TestConfigureSetsWALMaintenancePragmas(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "wal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings := walSettings{
		AutocheckpointPages:    123,
		JournalSizeLimitBytes:  4 * 1024 * 1024,
		TruncateThresholdBytes: 4 * 1024 * 1024,
	}
	if err := configure(db, 5*time.Second, true, settings); err != nil {
		t.Fatal(err)
	}
	var autocheckpoint int
	if err := db.QueryRow(`PRAGMA wal_autocheckpoint`).Scan(&autocheckpoint); err != nil {
		t.Fatal(err)
	}
	if autocheckpoint != settings.AutocheckpointPages {
		t.Fatalf("wal_autocheckpoint=%d want %d", autocheckpoint, settings.AutocheckpointPages)
	}
	var limit int64
	if err := db.QueryRow(`PRAGMA journal_size_limit`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != settings.JournalSizeLimitBytes {
		t.Fatalf("journal_size_limit=%d want %d", limit, settings.JournalSizeLimitBytes)
	}
}

func TestCheckpointWALTruncatesLargeWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	settings := walSettings{
		AutocheckpointPages:    0,
		JournalSizeLimitBytes:  0,
		TruncateThresholdBytes: 1,
	}
	if err := configure(db, 5*time.Second, true, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := db.Exec(`INSERT INTO items(value) VALUES (zeroblob(4096))`); err != nil {
			t.Fatal(err)
		}
	}
	size, exists, err := WALSize(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || size <= 0 {
		t.Fatalf("expected WAL file before checkpoint, exists=%v size=%d", exists, size)
	}
	var events []versionLogEvent
	if err := CheckpointWAL(db, path, settings.TruncateThresholdBytes, collectVersionLogEvents(&events)); err != nil {
		t.Fatal(err)
	}
	size, _, err = WALSize(path)
	if err != nil {
		t.Fatal(err)
	}
	if size > settings.TruncateThresholdBytes {
		t.Fatalf("WAL was not truncated: size=%d threshold=%d", size, settings.TruncateThresholdBytes)
	}
	if !hasCheckpointMode(events, "TRUNCATE") {
		t.Fatalf("expected truncate checkpoint log, got %+v", events)
	}
}

func TestCheckpointWALSkipsTruncateWhenPassiveIncomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	settings := walSettings{
		AutocheckpointPages:    0,
		JournalSizeLimitBytes:  0,
		TruncateThresholdBytes: 1,
	}
	if err := configure(db, 5*time.Second, true, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items(value) VALUES (zeroblob(4096))`); err != nil {
		t.Fatal(err)
	}

	readTx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	var count int
	if err := readTx.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 200; i++ {
		if _, err := db.Exec(`INSERT INTO items(value) VALUES (zeroblob(4096))`); err != nil {
			t.Fatal(err)
		}
	}

	var events []versionLogEvent
	if err := CheckpointWAL(db, path, settings.TruncateThresholdBytes, collectVersionLogEvents(&events)); err != nil {
		t.Fatal(err)
	}
	if hasCheckpointMode(events, "TRUNCATE") {
		t.Fatalf("expected truncate checkpoint to be skipped, got %+v", events)
	}
	if !hasVersionLogMessage(events, "SQLite WAL checkpoint 未完全收敛") {
		t.Fatalf("expected incomplete checkpoint log, got %+v", events)
	}
	for _, event := range events {
		if event.Message == "SQLite WAL checkpoint 未完全收敛" &&
			event.LogFrames > 0 && event.CheckedFrames < event.LogFrames {
			return
		}
	}
	t.Fatalf("expected incomplete checkpoint frame counts, got %+v", events)
}
