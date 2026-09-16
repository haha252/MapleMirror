package assetstate

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestReconcilePublicPathsIgnoresRemovedTargetReplica(t *testing.T) {
	db := openAssetStateMaster(t)
	seedPublicPathSwitch(t, db)
	mustExecAssetState(t, db, `UPDATE target_inventory SET desired_state = 'remove'
		WHERE node_id = 'node-1' AND asset_id = 'asset-old'`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReconcilePublicPaths(context.Background(), tx, "p1"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertAssetState(t, db, "asset-new", "candidate")
	assertAssetState(t, db, "asset-old", "superseded")
}

func openAssetStateMaster(t *testing.T) *sql.DB {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedPublicPathSwitch(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExecAssetState(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 2, 0, 1, 'hash', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-old', 'p1', 1, 'latest', 0, '2026-01-01T00:00:00Z', 1, 'now')`)
	mustExecAssetState(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-new', 'p1', 2, 'latest', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExecAssetState(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-old', 'rel-old', 1, 'ffmpeg.zip', 'amd64', 10,
		'https://example.invalid/old.zip', 'sha256:old', 'candidate', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-new', 'rel-new', 1, 'ffmpeg.zip', 'amd64', 20,
		'https://example.invalid/new.zip', 'sha256:new', 'pending', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
		routing_ready, public_download_base_url, created_at, updated_at)
		VALUES ('node-1', '节点一', 'syncing', 1, '2026-01-01T00:00:00Z',
		1, 'https://node-1.example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z')`)
	mustExecAssetState(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-old', 'sha256:old', 10, '2026-01-01T00:00:01Z', 'verified')`)
	mustExecAssetState(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-old', 'required', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-new', 'required', 'now')`)
}

func assertAssetState(t *testing.T, db *sql.DB, assetID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT service_state FROM assets WHERE id = ?`, assetID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("asset state got=%q want=%q", got, want)
	}
}

func mustExecAssetState(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeReleaseRolloutWaitsForReplacementReplica(t *testing.T) {
	db := openAssetStateMaster(t)
	mustExecAssetState(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('rollout', 'Rollout', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-old', 'rollout', 1, 'v1', 0, '2026-01-01T00:00:00Z', 1, 'now'),
		       ('rel-new', 'rollout', 2, 'v2', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExecAssetState(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-old', 'rel-old', 1, 'app.zip', '', 10, 'https://example.test/v1', 'sha256:old', 'candidate', 'now'),
		       ('asset-new', 'rel-new', 2, 'app.zip', '', 20, 'https://example.test/v2', 'sha256:new', 'candidate', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
		routing_ready, public_download_base_url, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 1, '2026-02-01T00:00:00Z',
		1, 'https://node.example.test', 'now', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-old', 'required', 'now'),
		       ('node-1', 'asset-new', 'required', 'now')`)
	mustExecAssetState(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-old', 'sha256:old', 10, 'now', 'verified')`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := FinalizeReleaseRollout(context.Background(), tx, "rollout")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if changed {
		_ = tx.Rollback()
		t.Fatal("replacement without a verified replica must not evict the serving release")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertSelectedReleaseCount(t, db, "rollout", 2)

	mustExecAssetState(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'asset-new', 'sha256:new', 20, 'now', 'verified')`)
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	changed, err = FinalizeReleaseRollout(context.Background(), tx, "rollout")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if !changed {
		_ = tx.Rollback()
		t.Fatal("verified replacement should finalize release selection")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertSelectedReleaseCount(t, db, "rollout", 1)
	var selected int
	if err := db.QueryRow(`SELECT selected FROM releases WHERE id='rel-new'`).Scan(&selected); err != nil || selected != 1 {
		t.Fatalf("new release selected=%d err=%v", selected, err)
	}
}

func assertSelectedReleaseCount(t *testing.T, db *sql.DB, projectID string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM releases WHERE project_id=? AND selected=1`, projectID).Scan(&got); err != nil || got != want {
		t.Fatalf("selected releases got=%d want=%d err=%v", got, want, err)
	}
}
