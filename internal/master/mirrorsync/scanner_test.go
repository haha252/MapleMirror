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

type fakeGitHub struct{ releases []GitHubRelease }

func (f fakeGitHub) ListReleases(context.Context, string) ([]GitHubRelease, error) {
	return f.releases, nil
}

func TestScanRejectsBadDigestAndCreatesInventoryTasks(t *testing.T) {
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
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{{
		ID: 1, TagName: "v1", PublishedAt: time.Now(),
		Assets: []GitHubAsset{
			{ID: 1, Name: "app-amd64.zip", Size: 10, URL: "https://example.invalid/a", Digest: good},
			{ID: 2, Name: "app-arm64.zip", Size: 10, URL: "https://example.invalid/b", Digest: "md5:bad"},
			{ID: 3, Name: "note.txt", Size: 1, URL: "https://example.invalid/c", Digest: good},
		},
	}}}}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, AssetInclude: []string{"*.zip"}, ArchitectureRegex: "(amd64|arm64)",
	}}}
	summary, err := scanner.Scan(context.Background(), projects, "", "req-scan")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AcceptedAssets != 1 || summary.RejectedAssets != 2 {
		t.Fatalf("摘要门禁统计错误 accepted=%d rejected=%d", summary.AcceptedAssets, summary.RejectedAssets)
	}
	assertCount(t, db, "assets", 1)
	assertCount(t, db, "target_inventory", 1)
	assertCount(t, db, "node_tasks", 1)
}

func seedNode(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO nodes
		(id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'sha256:aa', 'syncing', 1, 0, 'now', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
		t.Fatalf("%s 数量错误 got=%d want=%d err=%v", table, got, want, err)
	}
}
