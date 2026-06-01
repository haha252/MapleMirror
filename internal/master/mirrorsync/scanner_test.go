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
	var system string
	if err := db.QueryRow(`SELECT system FROM assets WHERE id = 'p1:1:1'`).Scan(&system); err != nil || system != "" {
		t.Fatalf("未启用系统匹配时 system 应为空：system=%q err=%v", system, err)
	}
	assertCount(t, db, "target_inventory", 1)
	assertCount(t, db, "node_tasks", 1)
}

func TestScanExtractsNormalizedSystemWhenEnabled(t *testing.T) {
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
			{ID: 1, Name: "app-windows-amd64.zip", Size: 10, URL: "https://example.invalid/a", Digest: good},
			{ID: 2, Name: "app-linux-arm64.zip", Size: 10, URL: "https://example.invalid/b", Digest: good},
			{ID: 3, Name: "app-darwin-amd64.zip", Size: 10, URL: "https://example.invalid/c", Digest: good},
			{ID: 4, Name: "app-freebsd-amd64.zip", Size: 10, URL: "https://example.invalid/d", Digest: good},
		},
	}}}}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, AssetInclude: []string{"*.zip"}, ArchitectureRegex: "(amd64|arm64)",
		SystemMatchEnabled: true, SystemRegex: "(windows|linux|darwin|freebsd)",
	}}}
	summary, err := scanner.Scan(context.Background(), projects, "", "req-scan")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AcceptedAssets != 3 || summary.RejectedAssets != 1 {
		t.Fatalf("系统匹配统计错误 accepted=%d rejected=%d", summary.AcceptedAssets, summary.RejectedAssets)
	}
	assertCount(t, db, "assets", 3)
	assertCount(t, db, "target_inventory", 3)
	assertCount(t, db, "node_tasks", 3)
	assertAssetSystem(t, db, "p1:1:1", "win")
	assertAssetSystem(t, db, "p1:1:2", "linux")
	assertAssetSystem(t, db, "p1:1:3", "darwin")
}

func TestScanProjectsDisabledConfigImmediately(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)

	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()}}
	enabled := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, ArchitectureRegex: "(amd64)",
	}}}
	if _, err := scanner.Scan(context.Background(), enabled, "", "req-enabled"); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "projects", 1)
	assertCount(t, db, "target_inventory", 1)

	disabled := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: false,
		RetainVersions: 1, ArchitectureRegex: "(amd64)",
	}}}
	if _, err := scanner.Scan(context.Background(), disabled, "", "req-disabled"); err != nil {
		t.Fatal(err)
	}
	assertProjectEnabled(t, db, "p1", false)
	assertTargetState(t, db, "remove")
}

func TestScanProjectsRemovedConfigDisablesExistingProject(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)

	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()}}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, ArchitectureRegex: "(amd64)",
	}}}
	if _, err := scanner.Scan(context.Background(), projects, "", "req-enabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(context.Background(), config.Projects{}, "", "req-removed"); err != nil {
		t.Fatal(err)
	}
	assertProjectEnabled(t, db, "p1", false)
	assertTargetState(t, db, "remove")
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

func assertAssetSystem(t *testing.T, db *sql.DB, assetID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT system FROM assets WHERE id = ?`, assetID).Scan(&got); err != nil || got != want {
		t.Fatalf("资产系统错误 asset=%s got=%q want=%q err=%v", assetID, got, want, err)
	}
}

func assertProjectEnabled(t *testing.T, db *sql.DB, projectID string, want bool) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT enabled FROM projects WHERE id = ?`, projectID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if (got == 1) != want {
		t.Fatalf("项目启用状态错误 got=%d want=%v", got, want)
	}
}

func assertTargetState(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT desired_state FROM target_inventory LIMIT 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("目标库存状态错误 got=%q want=%q", got, want)
	}
}
