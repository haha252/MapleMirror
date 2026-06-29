package mirrorsync

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestResetProjectClearsProjectDerivedData(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecReset(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', 'p1', 'owner/one', 1, 1, 0, 1, 'hash', ?)`, now)
	mustExecReset(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', 'node-1', 'syncing', 0, 0, ?, ?)`, now, now)
	mustExecReset(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('p1:1', 'p1', 1, 'v1', 0, ?, 1, ?)`, now, now)
	mustExecReset(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes, source_url,
		digest_sha256, service_state, created_at)
		VALUES ('p1:1:1', 'p1:1', 1, 'app.zip', '', 10, 'https://example.invalid/a',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'candidate', ?)`, now)
	mustExecReset(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'p1:1:1', 'required', ?)`, now)
	mustExecReset(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'p1:1:1', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 10, ?, 'verified')`, now)
	mustExecReset(t, db, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, completed_at,
		error_message, attempts, updated_at, retry_after)
		VALUES ('task-1', 'node-1', 'asset_download', 'p1:1:1', 'pending', 'req-1', ?, NULL,
		NULL, 0, ?, NULL)`, now, now)
	mustExecReset(t, db, `INSERT INTO sync_scans
		(id, project_id, state, selected_releases, accepted_assets, rejected_assets,
		error_message, request_id, started_at, completed_at, next_allowed_scan_at)
		VALUES ('scan-1', 'p1', 'succeeded', 1, 1, 0, NULL, 'req-scan', ?, ?, NULL)`, now, now)
	mustExecReset(t, db, `INSERT INTO project_scan_state
		(project_id, enabled, config_hash, last_scan_started_at, last_scan_completed_at,
		last_scan_id, last_scan_state, next_scan_at, last_error_message, updated_at)
		VALUES ('p1', 1, 'hash', ?, ?, 'scan-1', 'succeeded', ?, NULL, ?)`, now, now, now, now)
	mustExecReset(t, db, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES ('2026-06-03', 'p1', 2, 1, 100)`)
	mustExecReset(t, db, `INSERT INTO daily_asset_stats
		(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('2026-06-03', 'p1:1:1', 2, 1, 100, ?)`, now)
	mustExecReset(t, db, `INSERT INTO daily_public_stats
		(stat_day, page_views, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('2026-06-03', 9, 2, 1, 100, ?)`, now)
	mustExecReset(t, db, `INSERT INTO public_stat_totals
		(id, page_views, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('global', 20, 2, 1, 100, ?)
		ON CONFLICT(id) DO UPDATE SET page_views = excluded.page_views,
		authorization_count = excluded.authorization_count,
		transfer_started_count = excluded.transfer_started_count,
		sent_bytes = excluded.sent_bytes,
		updated_at = excluded.updated_at`, now)
	mustExecReset(t, db, `INSERT INTO asset_stat_totals
		(asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('p1:1:1', 2, 1, 100, ?)`, now)
	mustExecReset(t, db, `INSERT INTO project_stat_totals
		(project_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('p1', 2, 1, 100, ?)`, now)
	mustExecReset(t, db, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at, max_bytes, range_limit, status, request_id, first_transfer_at)
		VALUES ('auth-1', 'p1:1:1', 'node-1', 'prefix', ?, ?, 10, 1, 'active', 'req-auth', ?)`, now, now, now)
	mustExecReset(t, db, `INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes, settled_bytes, status, created_at)
		VALUES ('auth-1', '2026-06-03', 10, 10, 0, 'active', ?)`, now)
	mustExecReset(t, db, `INSERT INTO traffic_events
		(node_id, event_sequence, authorization_id, node_request_id, master_request_id, sent_bytes, reported_at, accounted_at)
		VALUES ('node-1', 1, 'auth-1', 'node-req-1', 'master-req-1', 10, ?, ?)`, now, now)
	mustExecReset(t, db, `INSERT INTO challenges
		(id, kind, asset_id, client_prefix_key, nonce_hash, difficulty, expires_at, consumed_at, request_id)
		VALUES ('challenge-1', 'altcha', 'p1:1:1', 'prefix', 'nonce', 1, ?, NULL, 'req-challenge')`, now)
	mustExecReset(t, db, `INSERT INTO daily_traffic_stats
		(stat_day, scope_kind, scope_key, sent_bytes, updated_at)
		VALUES ('2026-06-03', 'ipv4_32', '192.0.2.1/32', 777, ?)`, now)

	store := Store{DB: db}
	if err := store.ResetProject(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}

	assertCount(t, db, "projects", 1)
	assertCount(t, db, "releases", 0)
	assertCount(t, db, "assets", 0)
	assertCount(t, db, "target_inventory", 0)
	assertCount(t, db, "node_tasks", 0)
	assertCount(t, db, "node_inventory", 0)
	assertCount(t, db, "sync_scans", 0)
	assertCount(t, db, "project_scan_state", 0)
	assertCount(t, db, "daily_project_stats", 0)
	assertCount(t, db, "daily_asset_stats", 0)
	assertCount(t, db, "asset_stat_totals", 0)
	assertCount(t, db, "project_stat_totals", 0)
	assertCount(t, db, "download_authorizations", 0)
	assertCount(t, db, "traffic_reservations", 0)
	assertCount(t, db, "traffic_events", 0)
	assertCount(t, db, "challenges", 0)
	assertCount(t, db, "daily_traffic_stats", 1)
	assertCount(t, db, "nodes", 1)
	assertPublicStatsAfterReset(t, db)
}

func mustExecReset(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func assertPublicStatsAfterReset(t *testing.T, db *sql.DB) {
	t.Helper()
	var views, auth, started, sent int64
	if err := db.QueryRow(`SELECT page_views, authorization_count,
		transfer_started_count, sent_bytes FROM daily_public_stats
		WHERE stat_day = '2026-06-03'`).Scan(&views, &auth, &started, &sent); err != nil {
		t.Fatal(err)
	}
	if views != 9 || auth != 0 || started != 0 || sent != 0 {
		t.Fatalf("项目重置后每日公开状态错误：views=%d auth=%d started=%d sent=%d",
			views, auth, started, sent)
	}
	if err := db.QueryRow(`SELECT page_views, authorization_count,
		transfer_started_count, sent_bytes FROM public_stat_totals
		WHERE id = 'global'`).Scan(&views, &auth, &started, &sent); err != nil {
		t.Fatal(err)
	}
	if views != 20 || auth != 0 || started != 0 || sent != 0 {
		t.Fatalf("项目重置后公开累计状态错误：views=%d auth=%d started=%d sent=%d",
			views, auth, started, sent)
	}
}
