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

func TestScanPreservesServingReleaseUntilReplacementVerified(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	const oldDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const newDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	oldRelease := GitHubRelease{ID: 1, TagName: "v1", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 11, Name: "app.zip", Size: 10, URL: "https://example.invalid/v1", Digest: oldDigest}}}
	project := config.Project{ID: "rollout", Name: "Rollout", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, AssetInclude: config.AssetRules{{Pattern: "*.zip", Type: "glob"}}}
	projects := config.Projects{Projects: []config.Project{project}}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{oldRelease}}}
	if _, err := scanner.Scan(context.Background(), projects, "rollout", "old"); err != nil {
		t.Fatal(err)
	}
	mustExecScanner(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'rollout:1:11', '`+oldDigest+`', 10, 'now', 'verified')`)
	mustExecScanner(t, db, `UPDATE nodes SET state='online', last_heartbeat_at='now',
		public_download_base_url='https://node.example.test' WHERE id='node-1'`)

	newRelease := GitHubRelease{ID: 2, TagName: "v2", PublishedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 22, Name: "app.zip", Size: 20, URL: "https://example.invalid/v2", Digest: newDigest}}}
	scanner.GitHub = fakeGitHub{releases: []GitHubRelease{newRelease, oldRelease}}
	if _, err := scanner.Scan(context.Background(), projects, "rollout", "new"); err != nil {
		t.Fatal(err)
	}
	assertWhereCount(t, db, "releases", "project_id='rollout' AND selected=1", 2)
	assertWhereCount(t, db, "target_inventory", "node_id='node-1' AND asset_id='rollout:1:11' AND desired_state='required'", 1)
	assertWhereCount(t, db, "target_inventory", "node_id='node-1' AND asset_id='rollout:2:22' AND desired_state='required'", 1)
	assertWhereCount(t, db, "node_tasks", "node_id='node-1' AND asset_id='rollout:1:11' AND task_type='asset_delete' AND state IN ('pending','sent','running','retry_wait')", 0)
	assertWhereCount(t, db, "node_tasks", "node_id='node-1' AND asset_id='rollout:2:22' AND task_type='asset_download' AND state='pending'", 1)
}

func mustExecScanner(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}
