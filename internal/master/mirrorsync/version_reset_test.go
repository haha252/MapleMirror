package mirrorsync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestVersionResetAcceptsUpstreamWithdrawalWithoutClearingStats(t *testing.T) {
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
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "initial"); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecReset(t, db, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES ('2026-06-29', 'electerm', 7, 5, 1234)`)
	mustExecReset(t, db, `INSERT INTO project_stat_totals
		(project_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('electerm', 7, 5, 1234, ?)`, now)
	mustExecReset(t, db, `INSERT INTO daily_asset_stats
		(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('2026-06-29', 'electerm:90:900', 7, 5, 1234, ?)`, now)
	mustExecReset(t, db, `INSERT INTO asset_stat_totals
		(asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('electerm:90:900', 7, 5, 1234, ?)`, now)

	release86 := GitHubRelease{ID: 86, TagName: "v3.15.86",
		PublishedAt: time.Date(2026, 6, 26, 3, 3, 56, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 860, Name: "electerm-3.15.86-linux-amd64.deb",
			Size: 10, URL: "https://example.invalid/86", Digest: digest}}}
	scanner.GitHub = verifyingFakeGitHub{releases: []GitHubRelease{release86}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "ordinary"); err == nil ||
		!strings.Contains(err.Error(), "v3.15.90") {
		t.Fatalf("ordinary scan should reject upstream withdrawal, err=%v", err)
	}

	if _, err := scanner.ScanVersionReset(context.Background(), projects, "electerm", "version-reset"); err != nil {
		t.Fatalf("version reset should accept current upstream snapshot: %v", err)
	}
	assertSelectedTags(t, db, []string{"v3.15.86"})
	assertWhereCount(t, db, "releases", "project_id='electerm'", 2)
	assertWhereCount(t, db, "releases", "id='electerm:90' AND selected=0", 1)
	assertWhereCount(t, db, "assets", "id='electerm:90:900'", 1)

	var auth, started, sent int64
	if err := db.QueryRow(`SELECT authorization_count, transfer_started_count, sent_bytes
		FROM daily_project_stats WHERE project_id='electerm' AND stat_day='2026-06-29'`).
		Scan(&auth, &started, &sent); err != nil || auth != 7 || started != 5 || sent != 1234 {
		t.Fatalf("daily project stats changed: auth=%d started=%d sent=%d err=%v", auth, started, sent, err)
	}
	if err := db.QueryRow(`SELECT authorization_count, transfer_started_count, sent_bytes
		FROM project_stat_totals WHERE project_id='electerm'`).
		Scan(&auth, &started, &sent); err != nil || auth != 7 || started != 5 || sent != 1234 {
		t.Fatalf("project totals changed: auth=%d started=%d sent=%d err=%v", auth, started, sent, err)
	}
	if err := db.QueryRow(`SELECT authorization_count, transfer_started_count, sent_bytes
		FROM daily_asset_stats WHERE asset_id='electerm:90:900' AND stat_day='2026-06-29'`).
		Scan(&auth, &started, &sent); err != nil || auth != 7 || started != 5 || sent != 1234 {
		t.Fatalf("daily asset stats changed: auth=%d started=%d sent=%d err=%v", auth, started, sent, err)
	}
	if err := db.QueryRow(`SELECT authorization_count, transfer_started_count, sent_bytes
		FROM asset_stat_totals WHERE asset_id='electerm:90:900'`).
		Scan(&auth, &started, &sent); err != nil || auth != 7 || started != 5 || sent != 1234 {
		t.Fatalf("asset totals changed: auth=%d started=%d sent=%d err=%v", auth, started, sent, err)
	}
}

func TestVersionResetKeepsCurrentVersionWhenUpstreamFetchFails(t *testing.T) {
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
		Enabled: true, RetainVersions: 1}
	projects := config.Projects{Projects: []config.Project{project}}
	release90 := GitHubRelease{ID: 90, TagName: "v3.15.90",
		PublishedAt: time.Date(2026, 6, 29, 4, 46, 46, 0, time.UTC),
		Assets: []GitHubAsset{{ID: 900, Name: "electerm-3.15.90-linux-amd64.deb",
			Size: 10, URL: "https://example.invalid/90", Digest: digest}}}
	scanner := Scanner{Store: Store{DB: db}, GitHub: verifyingFakeGitHub{
		releases: []GitHubRelease{release90},
	}}
	if _, err := scanner.Scan(context.Background(), projects, "electerm", "initial"); err != nil {
		t.Fatal(err)
	}

	scanner.GitHub = verifyingFakeGitHub{err: errors.New("upstream unavailable")}
	if _, err := scanner.ScanVersionReset(context.Background(), projects, "electerm", "failed-reset"); err == nil {
		t.Fatal("version reset should fail when upstream fetch fails")
	}
	assertSelectedTags(t, db, []string{"v3.15.90"})
	assertWhereCount(t, db, "releases", "id='electerm:90' AND selected=1", 1)
}
