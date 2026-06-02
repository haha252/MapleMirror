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

func TestScanKeepsAssetWhenArchitectureMissing(t *testing.T) {
	db := openScannerTestDB(t)
	defer db.Close()
	seedNode(t, db)
	good := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{{
		ID: 1, TagName: "v1", PublishedAt: time.Now(),
		Assets: []GitHubAsset{
			{ID: 1, Name: "ZalithLauncher-2.4.4.apk", Size: 10, URL: "https://example.invalid/a", Digest: good},
			{ID: 2, Name: "ZalithLauncher-2.4.4-x86_64.apk", Size: 10, URL: "https://example.invalid/b", Digest: good},
		},
	}}}}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1,
		AssetInclude: config.AssetRules{{
			Pattern: `\.apk$`,
			Type:    "regex",
		}},
		ArchitectureRegex: `(?i)(?:^|[-_])(all|arm64-v8a|armeabi-v7a|x86_64|x86)(?:\.apk$|[-_.])`,
	}}}
	summary, err := scanner.Scan(context.Background(), projects, "", "req-scan")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AcceptedAssets != 2 || summary.RejectedAssets != 0 {
		t.Fatalf("架构提取统计错误 accepted=%d rejected=%d", summary.AcceptedAssets, summary.RejectedAssets)
	}
	assertCount(t, db, "assets", 2)
	assertAssetArchitecture(t, db, "p1:1:1", "None")
	assertAssetArchitecture(t, db, "p1:1:2", "x86_64")
}

func TestScanExtractsNormalizedSystemWhenEnabled(t *testing.T) {
	db := openScannerTestDB(t)
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
			{ID: 5, Name: "app-amd64.zip", Size: 10, URL: "https://example.invalid/e", Digest: good},
		},
	}}}}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, AssetInclude: config.AssetRules{{Pattern: "*.zip", Type: "glob"}}, ArchitectureRegex: "(amd64|arm64)",
		SystemMatchEnabled: true, SystemRegex: "(windows|linux|darwin|freebsd)",
	}}}
	summary, err := scanner.Scan(context.Background(), projects, "", "req-scan")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AcceptedAssets != 5 || summary.RejectedAssets != 0 {
		t.Fatalf("系统匹配统计错误 accepted=%d rejected=%d", summary.AcceptedAssets, summary.RejectedAssets)
	}
	assertCount(t, db, "assets", 5)
	assertCount(t, db, "target_inventory", 5)
	assertCount(t, db, "node_tasks", 5)
	assertAssetSystem(t, db, "p1:1:1", "win")
	assertAssetSystem(t, db, "p1:1:2", "linux")
	assertAssetSystem(t, db, "p1:1:3", "darwin")
	assertAssetSystem(t, db, "p1:1:4", "None")
	assertAssetSystem(t, db, "p1:1:5", "None")
}

func TestScanAssetRuleIncludeExcludeSemantics(t *testing.T) {
	db := openScannerTestDB(t)
	defer db.Close()
	seedNode(t, db)
	good := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{{
		ID: 1, TagName: "v1", PublishedAt: time.Now(),
		Assets: []GitHubAsset{
			{ID: 1, Name: "app-release.apk", Size: 10, URL: "https://example.invalid/a", Digest: good},
			{ID: 2, Name: "app-release-debug.apk", Size: 10, URL: "https://example.invalid/b", Digest: good},
			{ID: 3, Name: "app-release.txt", Size: 10, URL: "https://example.invalid/c", Digest: good},
			{ID: 4, Name: "app-beta.apk", Size: 10, URL: "https://example.invalid/d", Digest: good},
		},
	}}}}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1,
		AssetInclude: config.AssetRules{
			{Pattern: `^app-`, Type: "regex", Required: true},
			{Pattern: `release`, Type: "regex"},
		},
		AssetExclude: config.AssetRules{
			{Pattern: `debug`, Type: "regex"},
			{Pattern: `^app-`, Type: "regex", Required: true},
			{Pattern: `\.txt$`, Type: "regex", Required: true},
		},
		ArchitectureRegex: "(amd64)",
	}}}
	summary, err := scanner.Scan(context.Background(), projects, "", "req-scan")
	if err != nil {
		t.Fatal(err)
	}
	if summary.AcceptedAssets != 1 || summary.RejectedAssets != 3 {
		t.Fatalf("资产规则统计错误 accepted=%d rejected=%d", summary.AcceptedAssets, summary.RejectedAssets)
	}
	assertCount(t, db, "assets", 1)
}

func openScannerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func assertAssetSystem(t *testing.T, db *sql.DB, assetID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT system FROM assets WHERE id = ?`, assetID).Scan(&got); err != nil || got != want {
		t.Fatalf("资产系统错误 asset=%s got=%q want=%q err=%v", assetID, got, want, err)
	}
}

func assertAssetArchitecture(t *testing.T, db *sql.DB, assetID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT architecture FROM assets WHERE id = ?`, assetID).Scan(&got); err != nil || got != want {
		t.Fatalf("资产架构错误 asset=%s got=%q want=%q err=%v", assetID, got, want, err)
	}
}
