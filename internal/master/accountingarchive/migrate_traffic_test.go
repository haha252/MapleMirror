package accountingarchive

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestMigrateTrafficEventsArchivesAndDeletesRows(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedMigrationTraffic(t, db)
	root := t.TempDir()
	result, err := MigrateTrafficEvents(context.Background(), db,
		TrafficMigrationOptions{Root: root, BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArchivedRows != 1 {
		t.Fatalf("archived_rows=%d want 1", result.ArchivedRows)
	}
	assertCount(t, db, "traffic_events", 0)
	assertCount(t, db, "traffic_event_dedupe", 1)
	assertCount(t, db, "node_traffic_cursors", 1)
	done, err := TrafficArchiveMigrationComplete(context.Background(), db)
	if err != nil || !done {
		t.Fatalf("迁移完成标记错误：done=%v err=%v", done, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "traffic", "2026-05-29.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"event_type":"traffic_event"`) ||
		!strings.Contains(string(data), `"authorization_id":"auth-1"`) {
		t.Fatalf("归档内容缺少旧流量事件：%s", data)
	}
	if err := WriteTrafficManifest(root, result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "traffic", "migration-summary.json")); err != nil {
		t.Fatal(err)
	}
}

func seedMigrationTraffic(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	statements := []string{
		`INSERT INTO projects
			(id, name, repository, enabled, retain_versions, include_prerelease,
			download_multiplier, config_hash, updated_at)
			VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', '` + now + `')`,
		`INSERT INTO releases
			(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
			VALUES ('rel-1', 'p1', 1, 'v1', 0, '` + now + `', 1, '` + now + `')`,
		`INSERT INTO assets
			(id, release_id, github_asset_id, file_name, architecture, size_bytes,
			source_url, digest_sha256, service_state, created_at)
			VALUES ('asset-1', 'rel-1', 1, 'a.zip', 'amd64', 12,
			'https://example.test/a.zip', 'sha256:aa', 'candidate', '` + now + `')`,
		`INSERT INTO nodes
			(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
			routing_ready, created_at, updated_at)
			VALUES ('node-1', '节点一', 'ready', 1, '` + now + `', 1, '` + now + `', '` + now + `')`,
		`INSERT INTO download_authorizations
			(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
			max_bytes, range_limit, status, request_id)
			VALUES ('auth-1', 'asset-1', 'node-1', '192.0.2.1/32',
			'` + now + `', '` + now + `', 12, 4, 'active', 'master-req-1')`,
		`INSERT INTO traffic_events
			(node_id, event_sequence, authorization_id, node_request_id,
			master_request_id, sent_bytes, reported_at, accounted_at, asset_id, status)
			VALUES ('node-1', 1, 'auth-1', 'node-req-1', 'master-req-1',
			5, '` + now + `', '` + now + `', 'asset-1', 'completed')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d want %d", table, got, want)
	}
}
