package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterBackfillsProjectStatTotalsForExistingV11Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 11, '` + now + `')`,
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
		`CREATE TABLE daily_project_stats (
			stat_day TEXT NOT NULL,
			project_id TEXT NOT NULL,
			authorization_count INTEGER NOT NULL,
			transfer_started_count INTEGER NOT NULL,
			sent_bytes INTEGER NOT NULL,
			web_authorization_count INTEGER NOT NULL DEFAULT 0,
			api_authorization_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (stat_day, project_id)
		)`,
		`INSERT INTO projects
			(id, name, repository, enabled, retain_versions, include_prerelease,
			download_multiplier, config_hash, updated_at)
			VALUES ('p1', '项目一', 'owner/one', 1, 1, 0, 1, 'hash', '` + now + `')`,
		`INSERT INTO daily_project_stats
			(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes,
			web_authorization_count, api_authorization_count)
			VALUES ('2026-06-27', 'p1', 7, 4, 1024, 5, 2)`,
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
	assertTable(t, opened, "project_stat_totals")
	assertIndex(t, opened, "idx_project_stat_totals_downloads")
	assertDBVersion(t, opened, "master", masterDBVersion)

	var total, web, api, started, sent int64
	if err := opened.QueryRow(`SELECT authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes
		FROM project_stat_totals WHERE project_id = 'p1'`).
		Scan(&total, &web, &api, &started, &sent); err != nil {
		t.Fatal(err)
	}
	if total != 7 || web != 5 || api != 2 || started != 4 || sent != 1024 {
		t.Fatalf("project totals backfill mismatch: total=%d web=%d api=%d started=%d sent=%d",
			total, web, api, started, sent)
	}
}
