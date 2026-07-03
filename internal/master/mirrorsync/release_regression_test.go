package mirrorsync

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestScanKeepsKnownReleaseEvenWhenUpstreamDeletedIt(t *testing.T) {
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
		releases: []GitHubRelease{release90},
	}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "first"); err != nil {
		t.Fatal(err)
	}

	release86 := GitHubRelease{ID: 86, TagName: "v3.15.86",
		PublishedAt: time.Date(2026, 6, 26, 3, 3, 56, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 860, Name: "electerm-3.15.86-linux-amd64.deb",
			Size: 10, URL: "https://example.invalid/86", Digest: digest}}}
	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release86}}
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

	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release86}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "deleted"); err == nil {
		t.Fatal("上游删除旧 Release 后仍应保留已经观测到的版本")
	}
	if err := db.QueryRow(`SELECT tag_name FROM releases
		WHERE project_id = 'electerm' AND selected = 1`).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	if tag != "v3.15.90" {
		t.Fatalf("上游删除不得移除已知版本，got=%q", tag)
	}
}

func TestScanRejectsGapInsideRetainedReleaseWindow(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	project := config.Project{ID: "electerm", Name: "electerm", Repository: "electerm/electerm",
		Enabled: true, RetainVersions: 2}
	projects := config.Projects{Projects: []config.Project{project}}
	release := func(id int64, tag string, day int) GitHubRelease {
		return GitHubRelease{ID: id, TagName: tag,
			PublishedAt: time.Date(2026, 6, day, 4, 0, 0, 0, time.UTC)}
	}
	release100 := release(100, "v3.15.100", 30)
	release90 := release(90, "v3.15.90", 29)
	release86 := release(86, "v3.15.86", 26)
	scanner := Scanner{Store: Store{DB: db}, GitHub: verifyingFakeGitHub{
		releases: []GitHubRelease{release100, release90},
	}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "complete"); err != nil {
		t.Fatal(err)
	}

	scanner.GitHub = verifyingFakeGitHub{
		releases: []GitHubRelease{release100, release86},
	}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "incomplete"); err == nil ||
		!strings.Contains(err.Error(), "v3.15.90") {
		t.Fatalf("应拒绝缺少次新版本的快照，err=%v", err)
	}
	assertSelectedTags(t, db, []string{"v3.15.100", "v3.15.90"})

	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release100, release86}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "deleted"); err == nil {
		t.Fatal("上游删除次新 Release 后仍应拒绝不完整快照")
	}
	assertSelectedTags(t, db, []string{"v3.15.100", "v3.15.90"})

	release110 := release(110, "v3.15.110", 31)
	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release110, release100}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "advanced"); err != nil {
		t.Fatalf("新版本自然挤出旧版本时不应阻止更新：%v", err)
	}
	assertSelectedTags(t, db, []string{"v3.15.110", "v3.15.100"})
}

func assertSelectedTags(t *testing.T, db interface {
	Query(string, ...any) (*sql.Rows, error)
}, want []string) {
	t.Helper()
	rows, err := db.Query(`SELECT tag_name FROM releases
		WHERE project_id = 'electerm' AND selected = 1
		ORDER BY published_at DESC, github_release_id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			t.Fatal(err)
		}
		got = append(got, tag)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("已选版本不符：got=%v want=%v", got, want)
	}
}

type verifyingFakeGitHub struct {
	releases []GitHubRelease
}

func (f verifyingFakeGitHub) ListReleases(context.Context, string) ([]GitHubRelease, error) {
	return f.releases, nil
}
