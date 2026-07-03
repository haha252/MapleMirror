package mirrorsync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestScanConfirmsReleaseStillExistsBeforeRejectingRegression(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	project := config.Project{ID: "electerm", Name: "electerm", Repository: "electerm/electerm",
		Enabled: true, RetainVersions: 1, AssetInclude: config.AssetRules{{Pattern: "*.deb", Type: "glob"}}}
	projects := config.Projects{Projects: []config.Project{project}}
	release90 := GitHubRelease{ID: 90, TagName: "v3.15.90",
		PublishedAt: time.Date(2026, 6, 29, 4, 46, 46, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 900, Name: "electerm-3.15.90-linux-amd64.deb",
			Size: 10, URL: "https://example.invalid/90", Digest: digest}}}
	scanner := Scanner{Store: Store{DB: db}, GitHub: verifyingFakeGitHub{
		releases: []GitHubRelease{release90}, exists: true,
	}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "first"); err != nil {
		t.Fatal(err)
	}

	release86 := GitHubRelease{ID: 86, TagName: "v3.15.86",
		PublishedAt: time.Date(2026, 6, 26, 3, 3, 56, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 860, Name: "electerm-3.15.86-linux-amd64.deb",
			Size: 10, URL: "https://example.invalid/86", Digest: digest}}}
	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release86}, exists: true}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "stale"); err == nil ||
		!strings.Contains(err.Error(), "v3.15.90") {
		t.Fatalf("应拒绝回退快照，err=%v", err)
	}

	var tag string
	if err := db.QueryRow(`SELECT tag_name FROM releases
		WHERE project_id = 'electerm' AND selected = 1`).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	if tag != "v3.15.90" {
		t.Fatalf("回退快照不得覆盖已选版本，got=%q", tag)
	}

	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release86}, exists: false}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "deleted"); err != nil {
		t.Fatalf("旧 Release 已删除时应允许回退：%v", err)
	}
	if err := db.QueryRow(`SELECT tag_name FROM releases
		WHERE project_id = 'electerm' AND selected = 1`).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	if tag != "v3.15.86" {
		t.Fatalf("确认旧 Release 删除后应选中回退版本，got=%q", tag)
	}
}

type verifyingFakeGitHub struct {
	releases []GitHubRelease
	exists   bool
	err      error
}

func (f verifyingFakeGitHub) ListReleases(context.Context, string) ([]GitHubRelease, error) {
	return f.releases, nil
}

func (f verifyingFakeGitHub) ReleaseExists(context.Context, string, int64) (bool, error) {
	return f.exists, f.err
}
