package public

import (
	"context"
	"database/sql"
	"testing"
)

func TestTopResourcesMergesReuploadedSameVisibleFile(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db)
	seedRankAsset(t, db, "rel-old", 10, "asset-old", 100,
		"app-arm64.apk", "arm64", "android", 50)
	seedRankAsset(t, db, "rel-new", 11, "asset-new", 101,
		"app-arm64.apk", "arm64", "android", 51)
	store := Store{DB: db}

	resources, err := store.TopResources(context.Background(), "2026-06-01", "2026-06-03", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("同文件重传后热门排行应合并为 1 条，got=%d: %+v", len(resources), resources)
	}
	item := resources[0]
	if item.ProjectName != "项目一" || item.Version != "1.0.0" ||
		item.FileName != "app-arm64.apk" || item.Architecture != "arm64" ||
		item.System != "android" || item.DownloadCount != 101 {
		t.Fatalf("热门排行合并结果错误：%+v", item)
	}
}

func TestTopResourcesKeepsDifferentFilesSeparate(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db)
	seedRankAsset(t, db, "rel-old", 10, "asset-one", 100,
		"app-arm64.apk", "arm64", "android", 50)
	seedRankAsset(t, db, "rel-new", 11, "asset-two", 101,
		"app-arm64-extra.apk", "arm64", "android", 51)
	store := Store{DB: db}

	resources, err := store.TopResources(context.Background(), "2026-06-01", "2026-06-03", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 {
		t.Fatalf("不同文件不应被合并，got=%d: %+v", len(resources), resources)
	}
	if resources[0].FileName != "app-arm64-extra.apk" || resources[0].DownloadCount != 51 {
		t.Fatalf("第一名错误：%+v", resources[0])
	}
	if resources[1].FileName != "app-arm64.apk" || resources[1].DownloadCount != 50 {
		t.Fatalf("第二名错误：%+v", resources[1])
	}
}

func TestTopResourcesKeepsDifferentArchitecturesSeparate(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db)
	seedRankAsset(t, db, "rel-old", 10, "asset-arm64", 100,
		"app-arm64.apk", "arm64", "android", 50)
	seedRankAsset(t, db, "rel-new", 11, "asset-x64", 101,
		"app-arm64.apk", "x64", "android", 51)
	store := Store{DB: db}

	resources, err := store.TopResources(context.Background(), "2026-06-01", "2026-06-03", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 {
		t.Fatalf("不同架构不应被合并，got=%d: %+v", len(resources), resources)
	}
	if resources[0].Architecture != "x64" || resources[0].DownloadCount != 51 {
		t.Fatalf("第一名错误：%+v", resources[0])
	}
	if resources[1].Architecture != "arm64" || resources[1].DownloadCount != 50 {
		t.Fatalf("第二名错误：%+v", resources[1])
	}
}

func seedRankProject(t *testing.T, db execDB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 2, 0, 1, 'hash', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
}

func seedRankAsset(t *testing.T, db execDB, releaseID string, githubReleaseID int,
	assetID string, githubAssetID int, fileName, architecture, system string, downloads int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES (?, 'p1', ?, '1.0.0', 0, '2026-06-01T00:00:00Z', 1, 'now')`,
		releaseID, githubReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, system, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 12, 'https://example.test/a.apk',
		'sha256:aa', 'candidate', 'now')`,
		assetID, releaseID, githubAssetID, fileName, architecture, system)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO daily_asset_stats
		(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('2026-06-03', ?, ?, 0, 0, 'now')`, assetID, downloads)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO asset_stat_totals
		(asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, ?, 0, 0, 'now')`, assetID, downloads)
	if err != nil {
		t.Fatal(err)
	}
}

type execDB interface {
	Exec(query string, args ...any) (sql.Result, error)
}
