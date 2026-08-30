package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
)

func TestOpenMasterV19CreatesDeveloperAPITablesForExistingV18Database(t *testing.T) {
	wal := false
	path := filepath.Join(t.TempDir(), "master.db")
	cfg := config.Database{Path: path, BusyTimeout: "5s", WAL: &wal}
	db, err := OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP TABLE project_developer_api_daily_usage",
		"DROP TABLE project_developer_tokens",
		"UPDATE database_version SET version = 18 WHERE kind = 'master'",
	} {
		if _, err := raw.Exec(statement); err != nil {
			_ = raw.Close()
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	assertDBVersion(t, opened, "master", masterDBVersion)
	assertTable(t, opened, "project_developer_tokens")
	assertTable(t, opened, "project_developer_api_daily_usage")
	assertIndex(t, opened, "idx_project_developer_tokens_hash")
}
