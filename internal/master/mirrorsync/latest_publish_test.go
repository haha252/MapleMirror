package mirrorsync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestScanKeepsVerifiedLatestCandidateUntilNewReplicaReady(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	_, err = db.Exec(`UPDATE nodes SET last_heartbeat_at = '2026-01-01T00:00:01Z',
		public_download_base_url = 'https://node-1.example.com' WHERE id = 'node-1'`)
	if err != nil {
		t.Fatal(err)
	}

	good := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newer := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	oldRelease := GitHubRelease{ID: 1, TagName: "latest", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 1, Name: "ffmpeg.zip", Size: 10, URL: "https://example.invalid/old", Digest: good}}}
	project := config.Project{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 2, AssetInclude: config.AssetRules{{Pattern: "*.zip", Type: "glob"}},
	}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{oldRelease}}}
	if _, err := scanner.Scan(context.Background(), config.Projects{Projects: []config.Project{project}}, "", "req-old"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'p1:1:1', ?, 10, '2026-01-01T00:00:02Z', 'verified')`, good)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE node_tasks SET state = 'succeeded' WHERE asset_id = 'p1:1:1'`)
	if err != nil {
		t.Fatal(err)
	}

	newRelease := GitHubRelease{ID: 2, TagName: "latest", PublishedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 1, Name: "ffmpeg.zip", Size: 20, URL: "https://example.invalid/new", Digest: newer}}}
	scanner.GitHub = fakeGitHub{releases: []GitHubRelease{oldRelease, newRelease}}
	if _, err := scanner.Scan(context.Background(), config.Projects{Projects: []config.Project{project}}, "", "req-new"); err != nil {
		t.Fatal(err)
	}

	assertAssetState(t, db, "p1:1:1", "candidate")
	assertAssetState(t, db, "p1:2:1", "pending")
	assertWhereCount(t, db, "target_inventory", "desired_state = 'required'", 2)
	assertWhereCount(t, db, "node_tasks", "asset_id = 'p1:2:1' AND state = 'pending'", 1)
}
