package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterBackfillsDownloadSourceStatsForExistingV9Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE database_version (kind TEXT PRIMARY KEY, version INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO database_version(kind, version, updated_at) VALUES ('master', 9, '` + now + `')`,
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
		`CREATE TABLE daily_project_stats (
			stat_day TEXT NOT NULL,
			project_id TEXT NOT NULL,
			authorization_count INTEGER NOT NULL,
			transfer_started_count INTEGER NOT NULL,
			sent_bytes INTEGER NOT NULL,
			PRIMARY KEY (stat_day, project_id)
		)`,
		`CREATE TABLE daily_asset_stats (
			stat_day TEXT NOT NULL,
			asset_id TEXT NOT NULL,
			authorization_count INTEGER NOT NULL,
			transfer_started_count INTEGER NOT NULL,
			sent_bytes INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (stat_day, asset_id)
		)`,
		`INSERT INTO daily_project_stats
			(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
			VALUES ('2026-06-27', 'p1', 7, 4, 1024)`,
		`INSERT INTO daily_asset_stats
			(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
			VALUES ('2026-06-27', 'asset-1', 7, 4, 1024, '` + now + `')`,
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
	assertColumn(t, opened, "download_authorizations", "source_kind")
	assertColumn(t, opened, "daily_project_stats", "web_authorization_count")
	assertColumn(t, opened, "daily_project_stats", "api_authorization_count")
	assertColumn(t, opened, "daily_asset_stats", "web_authorization_count")
	assertColumn(t, opened, "daily_asset_stats", "api_authorization_count")
	assertDBVersion(t, opened, "master", 10)

	var web, api int64
	if err := opened.QueryRow(`SELECT web_authorization_count, api_authorization_count
		FROM daily_project_stats WHERE project_id = 'p1'`).Scan(&web, &api); err != nil {
		t.Fatal(err)
	}
	if web != 7 || api != 0 {
		t.Fatalf("project source backfill mismatch: web=%d api=%d", web, api)
	}
	if err := opened.QueryRow(`SELECT web_authorization_count, api_authorization_count
		FROM daily_asset_stats WHERE asset_id = 'asset-1'`).Scan(&web, &api); err != nil {
		t.Fatal(err)
	}
	if web != 7 || api != 0 {
		t.Fatalf("asset source backfill mismatch: web=%d api=%d", web, api)
	}
}
