package mirrorsync

import (
	"context"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestSyncStatusIncludesDetailedDiagnostics(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	_, err = db.Exec(`UPDATE nodes SET last_heartbeat_at = '2026-05-30T11:04:34Z' WHERE id = 'node-1'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease, download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes, source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'app.zip', 'amd64', 10, 'https://example.invalid', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'candidate', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at) VALUES ('node-1', 'asset-1', 'required', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	store := Store{DB: db}
	status, err := store.SyncStatus(context.Background(), "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.RoutingReadyReason != "尚未上报完整库存，未完成最终对账" {
		t.Fatalf("unexpected reason: %q", status.RoutingReadyReason)
	}
	if status.RoutingReadyDetail == "" || status.LastHeartbeatAt == "" {
		t.Fatalf("expected diagnostics fields, got %+v", status)
	}
}
