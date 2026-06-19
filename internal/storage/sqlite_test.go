package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

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
	assertColumn(t, db, "assets", "variant")
	assertColumn(t, db, "assets", "classification_reason")
	_ = db.Close()
	db, err = OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertDBVersion(t, db, "master", 3)
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
	assertTable(t, db, "node_enrollment_state")
	assertTable(t, db, "local_sync_tasks")
	assertTable(t, db, "pending_sync_task_results")
	assertColumn(t, db, "pending_sync_task_results", "peer_fallback_attempted")
	assertColumn(t, db, "control_identity", "certificate_pem")
	assertColumn(t, db, "control_identity", "ca_pem")
	assertColumn(t, db, "control_identity", "private_key_pem")
	assertColumn(t, db, "control_identity", "download_token_public_key_pem")
	assertColumn(t, db, "inventory_report_cursor", "force_report_requested_at")
	assertDBVersion(t, db, "node", 3)
}

func assertTable(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, table string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("缺少数据表 %s：%v", table, err)
	}
}

func assertColumn(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return
		}
	}
	t.Fatalf("缺少数据列 %s.%s", table, column)
}

func assertDBVersion(t *testing.T, db *sql.DB, kind string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT version FROM database_version WHERE kind = ?`, kind).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s database version=%d want=%d", kind, got, want)
	}
}
