package mirrorsync

import (
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func testMirrorSyncDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, func() { _ = db.Close() }
}

func assertMirrorSyncUpdatedAt(t *testing.T, db *sql.DB, table, where, want string) {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT updated_at FROM ` + table + ` WHERE ` + where).Scan(&got)
	if err != nil || got != want {
		t.Fatalf("%s updated_at=%q want %q err=%v", table, got, want, err)
	}
}

func mustExecMirrorSync(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatal(err)
	}
}
