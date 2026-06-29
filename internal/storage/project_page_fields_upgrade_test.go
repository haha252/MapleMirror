package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterBackfillsProjectPageFieldsForExistingV12Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 12, '` + now + `')`,
		`CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			repository TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL,
			retain_versions INTEGER NOT NULL,
			include_prerelease INTEGER NOT NULL,
			download_multiplier INTEGER NOT NULL,
			config_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`INSERT INTO projects
			(id, name, repository, enabled, retain_versions, include_prerelease,
			download_multiplier, config_hash, updated_at)
			VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', '` + now + `')`,
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
	assertColumn(t, opened, "projects", "description")
	assertColumn(t, opened, "projects", "homepage_url")
	assertDBVersion(t, opened, "master", masterDBVersion)
	var description, homepage string
	if err := opened.QueryRow(`SELECT description, homepage_url FROM projects WHERE id = 'p1'`).
		Scan(&description, &homepage); err != nil {
		t.Fatal(err)
	}
	if description != "" || homepage != "" {
		t.Fatalf("新增项目页面字段默认值错误 description=%q homepage=%q", description, homepage)
	}
}
