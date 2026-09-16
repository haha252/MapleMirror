package mirrorsync

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

const btbnDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBtbNRetainCountsMirrorableReleasesAndKeepsTwoLatestGenerations(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	mustExecScanner(t, db, `UPDATE nodes SET state='online', last_heartbeat_at='now',
		public_download_base_url='https://node.example.test' WHERE id='node-1'`)

	project := config.Project{ID: "FFmpeg", Name: "FFmpeg", Repository: "BtbN/FFmpeg-Builds",
		Enabled: true, RetainVersions: 2,
		AssetInclude: config.AssetRules{{Pattern: `(?i)^ffmpeg-master-latest-(linux64|linuxarm64|win64|winarm64)-gpl\.(zip|tar\.xz)$`, Type: "regex", Required: true}},
	}
	projects := config.Projects{Projects: []config.Project{project}}
	old := btbnLatestRelease(100, time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC))
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: []GitHubRelease{old}}}
	if _, err := scanner.Scan(context.Background(), projects, "FFmpeg", "old"); err != nil {
		t.Fatal(err)
	}
	verifyReleaseOnNode(t, db, 100)

	current := btbnLatestRelease(200, time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC))
	empty := btbnAutobuildRelease(190, time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC))
	scanner.GitHub = fakeGitHub{releases: []GitHubRelease{current, empty}}
	if _, err := scanner.Scan(context.Background(), projects, "FFmpeg", "current-pending"); err != nil {
		t.Fatal(err)
	}
	assertSelectedReleaseIDs(t, db, 200, 100)
	assertWhereCount(t, db, "releases", "github_release_id=190", 0)
	assertReleaseAssetStateCount(t, db, 100, "candidate", 4)
	assertReleaseAssetStateCount(t, db, 200, "pending", 4)
	assertWhereCount(t, db, "target_inventory", "desired_state='required'", 8)

	verifyReleaseOnNode(t, db, 200)
	if _, err := scanner.Scan(context.Background(), projects, "FFmpeg", "current-ready"); err != nil {
		t.Fatal(err)
	}
	assertSelectedReleaseIDs(t, db, 200, 100)
	assertReleaseAssetStateCount(t, db, 100, "superseded", 4)
	assertReleaseAssetStateCount(t, db, 200, "candidate", 4)
	assertWhereCount(t, db, "target_inventory", "desired_state='required'", 8)

	newest := btbnLatestRelease(300, time.Date(2026, 9, 16, 13, 0, 0, 0, time.UTC))
	nextEmpty := btbnAutobuildRelease(290, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	scanner.GitHub = fakeGitHub{releases: []GitHubRelease{newest, nextEmpty}}
	if _, err := scanner.Scan(context.Background(), projects, "FFmpeg", "newest-pending"); err != nil {
		t.Fatal(err)
	}
	assertSelectedReleaseIDs(t, db, 300, 200, 100)
	assertWhereCount(t, db, "target_inventory", "desired_state='required'", 12)

	verifyReleaseOnNode(t, db, 300)
	if _, err := scanner.Scan(context.Background(), projects, "FFmpeg", "newest-ready"); err != nil {
		t.Fatal(err)
	}
	assertSelectedReleaseIDs(t, db, 300, 200)
	assertReleaseAssetStateCount(t, db, 300, "candidate", 4)
	assertReleaseAssetStateCount(t, db, 200, "superseded", 4)
	assertWhereCount(t, db, "target_inventory", "desired_state='required'", 8)
	assertWhereCount(t, db, "target_inventory", "desired_state='remove'", 4)
}

func btbnLatestRelease(id int64, published time.Time) GitHubRelease {
	names := []string{
		"ffmpeg-master-latest-linux64-gpl.tar.xz",
		"ffmpeg-master-latest-linuxarm64-gpl.tar.xz",
		"ffmpeg-master-latest-win64-gpl.zip",
		"ffmpeg-master-latest-winarm64-gpl.zip",
	}
	assets := make([]GitHubAsset, 0, len(names))
	for i, name := range names {
		assets = append(assets, GitHubAsset{ID: id*10 + int64(i+1), Name: name, Size: 10,
			URL: "https://example.invalid/" + name, Digest: btbnDigest})
	}
	return GitHubRelease{ID: id, TagName: "latest", PublishedAt: published, Assets: assets}
}

func btbnAutobuildRelease(id int64, published time.Time) GitHubRelease {
	return GitHubRelease{ID: id, TagName: fmt.Sprintf("autobuild-%d", id), PublishedAt: published,
		Assets: []GitHubAsset{{ID: id*10 + 1, Name: "ffmpeg-N-123-linux64-gpl.tar.xz", Size: 10,
			URL: "https://example.invalid/autobuild", Digest: btbnDigest}}}
}

func verifyReleaseOnNode(t *testing.T, db *sql.DB, releaseID int64) {
	t.Helper()
	rows, err := db.Query(`SELECT a.id FROM assets a JOIN releases r ON r.id=a.release_id
		WHERE r.github_release_id=?`, releaseID)
	if err != nil {
		t.Fatal(err)
	}
	var assetIDs []string
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		assetIDs = append(assetIDs, assetID)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, assetID := range assetIDs {
		mustExecScanner(t, db, `INSERT OR REPLACE INTO node_inventory
			(node_id,asset_id,local_digest_sha256,size_bytes,verified_at,state)
			VALUES ('node-1','`+assetID+`','`+btbnDigest+`',10,'now','verified')`)
	}
}

func assertSelectedReleaseIDs(t *testing.T, db *sql.DB, want ...int64) {
	t.Helper()
	rows, err := db.Query(`SELECT github_release_id FROM releases WHERE project_id='FFmpeg' AND selected=1
		ORDER BY published_at DESC, github_release_id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("selected release ids got=%v want=%v", got, want)
	}
}

func assertReleaseAssetStateCount(t *testing.T, db *sql.DB, releaseID int64, state string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM assets a JOIN releases r ON r.id=a.release_id
		WHERE r.github_release_id=? AND a.service_state=?`, releaseID, state).Scan(&got); err != nil || got != want {
		t.Fatalf("release=%d state=%s got=%d want=%d err=%v", releaseID, state, got, want, err)
	}
}
