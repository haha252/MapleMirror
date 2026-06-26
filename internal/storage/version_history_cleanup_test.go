package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func TestOpenMasterV9CleansBoundedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.db")
	db, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	if err := seedV9CleanupData(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE database_version SET version = 8 WHERE kind = 'master'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	assertDBVersion(t, opened, "master", 9)
	assertCountAtMost(t, opened, "node_control_sessions", 1000)
	assertCountAtMost(t, opened, "node_inventory_reports", 200)

	var minSeq int
	if err := opened.QueryRow(`SELECT MIN(event_sequence) FROM traffic_event_dedupe`).Scan(&minSeq); err != nil {
		t.Fatal(err)
	}
	if minSeq < 2000 {
		t.Fatalf("traffic_event_dedupe min sequence=%d, want >= 2000", minSeq)
	}
}

func seedV9CleanupData(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -10).Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', ?)`, old); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, ?, 1, ?)`, old, old); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'a.zip', 'amd64', 12,
		'https://example.test/a.zip', 'sha256:aa', 'candidate', ?)`, old); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 1, 1, ?, ?)`, old, old); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at, max_bytes,
		range_limit, status, request_id)
		VALUES ('auth-1', 'asset-1', 'node-1', '192.0.2.1/32', ?, ?, 1, 1, 'active', 'req')`,
		old, now.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO node_traffic_cursors
		(node_id, last_event_sequence, updated_at) VALUES ('node-1', 7000, ?)`, old); err != nil {
		return err
	}
	for i := 1; i <= 7000; i++ {
		if _, err := tx.Exec(`INSERT INTO traffic_event_dedupe
			(node_id, event_sequence, authorization_id, event_hash, accounted_at)
			VALUES ('node-1', ?, 'auth-1', ?, ?)`, i, fmt.Sprintf("hash-%d", i), old); err != nil {
			return err
		}
	}
	for i := 1; i <= 1100; i++ {
		if _, err := tx.Exec(`INSERT INTO node_control_sessions
			(id, node_id, request_id, connected_at, last_message_sequence, disconnected_at, close_reason)
			VALUES (?, 'node-1', 'req', ?, 0, ?, 'test')`,
			fmt.Sprintf("sess-%04d", i), old, old); err != nil {
			return err
		}
	}
	for i := 1; i <= 250; i++ {
		if _, err := tx.Exec(`INSERT INTO node_inventory_reports
			(id, node_id, revision, complete, item_count, result, request_id, reported_at)
			VALUES (?, 'node-1', ?, 1, 1, 'accepted', 'req', ?)`,
			fmt.Sprintf("report-%04d", i), i, old); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func assertCountAtMost(t *testing.T, db *sql.DB, table string, max int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > max {
		t.Fatalf("%s count=%d, want <= %d", table, count, max)
	}
}
