package mirrorsync

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	publicstore "mirror-server/internal/master/public"
	"mirror-server/internal/storage"
)

func TestScanMarksUnacceptedReleaseAssetRemoved(t *testing.T) {
	good := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name         string
		secondAssets []GitHubAsset
	}{
		{name: "deleted upstream"},
		{name: "rejected by rules", secondAssets: []GitHubAsset{{
			ID: 1, Name: "foo.txt", Size: 10, URL: "https://example.invalid/foo", Digest: good,
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, scanner, projects, release := seedAcceptedReleaseAsset(t, good)
			defer db.Close()
			release.Assets = tt.secondAssets
			scanner.GitHub = fakeGitHub{releases: []GitHubRelease{release}}
			if _, err := scanner.Scan(context.Background(), projects, "", "req-second"); err != nil {
				t.Fatal(err)
			}

			assertAssetState(t, db, "p1:1:1", "removed")
			assertWhereCount(t, db, "target_inventory", "asset_id = 'p1:1:1' AND desired_state = 'remove'", 1)
			assertWhereCount(t, db, "node_tasks", "asset_id = 'p1:1:1' AND state = 'obsolete'", 1)
			assertWhereCount(t, db, "node_tasks", "asset_id = 'p1:1:1' AND task_type = 'asset_delete' AND state = 'pending'", 1)
			assertRemovedAssetIsPubliclyInvisible(t, db)
		})
	}
}

func seedAcceptedReleaseAsset(t *testing.T, digest string) (*sql.DB, Scanner, config.Projects, GitHubRelease) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedRoutableNode(t, db)
	project := config.Project{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, AssetInclude: config.AssetRules{{Pattern: "*.zip", Type: "glob"}},
	}
	release := GitHubRelease{ID: 1, TagName: "v1", PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 1, Name: "foo.zip", Size: 10, URL: "https://example.invalid/foo", Digest: digest}}}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{release}}}
	projects := config.Projects{Projects: []config.Project{project}}
	if _, err := scanner.Scan(context.Background(), projects, "", "req-first"); err != nil {
		t.Fatal(err)
	}
	seedVerifiedAsset(t, db, digest)
	return db, scanner, projects, release
}

func seedRoutableNode(t *testing.T, db *sql.DB) {
	t.Helper()
	seedNode(t, db)
	_, err := db.Exec(`UPDATE nodes SET last_heartbeat_at = '2026-01-01T00:00:01Z',
		public_download_base_url = 'https://node-1.example.com' WHERE id = 'node-1'`)
	if err != nil {
		t.Fatal(err)
	}
}

func seedVerifiedAsset(t *testing.T, db *sql.DB, digest string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'p1:1:1', ?, 10, '2026-01-01T00:00:02Z', 'verified')`, digest)
	if err != nil {
		t.Fatal(err)
	}
}

func assertRemovedAssetIsPubliclyInvisible(t *testing.T, db *sql.DB) {
	t.Helper()
	store := publicstore.Store{DB: db}
	assets, err := store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 0 {
		t.Fatalf("removed asset should be hidden from catalog: %#v", assets)
	}
	_, err = store.DownloadAsset(context.Background(), "p1:1:1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("removed asset should not resolve as public download: %v", err)
	}
	_, err = store.CreateChallenge(context.Background(), "altcha", "p1:1:1", "client", 1, time.Minute, "req")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("removed asset should not be authorizable: %v", err)
	}
}
