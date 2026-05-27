package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
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
	_ = db.Close()
	db, err = OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != 4 {
		t.Fatalf("主节点迁移重复执行不符合预期：count=%d err=%v", count, err)
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
}

func assertTable(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, table string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("缺少数据表 %s：%v", table, err)
	}
}
