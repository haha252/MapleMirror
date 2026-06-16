package mirrorsync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestScanMarksVerifiedInventoryStaleWhenSameGitHubAssetChanges(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	good := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newer := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	project := config.Project{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, AssetInclude: config.AssetRules{{Pattern: "*.zip", Type: "glob"}},
	}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{{
		ID: 1, TagName: "v1", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 1, Name: "app.zip", Size: 10, URL: "https://example.invalid/old", Digest: good}},
	}}}}
	projects := config.Projects{Projects: []config.Project{project}}
	if _, err := scanner.Scan(context.Background(), projects, "", "req-old"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'p1:1:1', ?, 10, '2026-01-01T00:00:01Z', 'verified')`, good)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE node_tasks SET state = 'succeeded' WHERE asset_id = 'p1:1:1'`)
	if err != nil {
		t.Fatal(err)
	}

	scanner.GitHub = fakeGitHub{releases: []GitHubRelease{{
		ID: 1, TagName: "v1", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 1, Name: "app.zip", Size: 20, URL: "https://example.invalid/new", Digest: newer}},
	}}}
	if _, err := scanner.Scan(context.Background(), projects, "", "req-new"); err != nil {
		t.Fatal(err)
	}

	assertInventoryState(t, db, "node-1", "p1:1:1", "stale")
	assertWhereCount(t, db, "node_tasks", "asset_id = 'p1:1:1' AND state = 'pending'", 1)
}
