package public

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/master/assetstate"
)

func TestLatestPathSwitchesAfterPendingAssetVerified(t *testing.T) {
	db := openMaster(t)
	seedLatestSwitchState(t, db)
	store := Store{DB: db}

	assertDownloadPathAsset(t, store, "asset-old")
	assertAuthorizationAsset(t, store, "asset-old", "https://node-1.example.com/p1/latest/ffmpeg.zip")

	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-new', 'sha256:new', 20, '2026-02-01T00:00:01Z', 'verified')`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := assetstate.ReconcilePublicPaths(context.Background(), tx, "p1"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertDownloadPathAsset(t, store, "asset-new")
	assertAuthorizationAsset(t, store, "asset-new", "https://node-1.example.com/p1/latest/ffmpeg.zip")
}

func seedLatestSwitchState(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 2, 0, 1, 'hash', 'now')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-old', 'p1', 1, 'latest', 0, '2026-01-01T00:00:00Z', 1, 'now')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-new', 'p1', 2, 'latest', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-old', 'rel-old', 1, 'ffmpeg.zip', 'amd64', 10,
		'https://example.test/old.zip', 'sha256:old', 'candidate', 'now')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-new', 'rel-new', 1, 'ffmpeg.zip', 'amd64', 20,
		'https://example.test/new.zip', 'sha256:new', 'pending', 'now')`)
	mustExec(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
		routing_ready, public_download_base_url, created_at, updated_at)
		VALUES ('node-1', '节点一', 'syncing', 1, '2026-01-01T00:00:00Z',
		1, 'https://node-1.example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z')`)
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-old', 'sha256:old', 10, '2026-01-01T00:00:01Z', 'verified')`)
}

func assertDownloadPathAsset(t *testing.T, store Store, want string) {
	t.Helper()
	asset, err := store.DownloadAssetByPath(context.Background(), "/p1/latest/ffmpeg.zip")
	if err != nil || asset.AssetID != want {
		t.Fatalf("公开路径资产错误 asset=%+v want=%s err=%v", asset, want, err)
	}
}

func assertAuthorizationAsset(t *testing.T, store Store, wantAsset, wantURL string) {
	t.Helper()
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		wantAsset, "192.0.2.1/32", 4, time.Minute, "req-challenge")
	if err != nil {
		t.Fatal(err)
	}
	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-auth")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.AssetID != wantAsset || debug.DownloadURL != wantURL {
		t.Fatalf("授权绑定错误 claims=%+v debug=%+v want_asset=%s want_url=%s",
			auth.Claims, debug, wantAsset, wantURL)
	}
}
