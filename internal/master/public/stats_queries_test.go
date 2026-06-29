package public

import (
	"context"
	"database/sql"
	"testing"
)

func TestTopProjectsAggregatesAllProjectVersionsIntoSingleRank(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db, "p1", "项目一", true)
	seedRankReleaseAsset(t, db, "p1", "rel-old", 10, "asset-old", 100,
		"app-arm64.apk", "arm64", "android")
	seedRankReleaseAsset(t, db, "p1", "rel-new", 11, "asset-new", 101,
		"app-arm64-extra.apk", "x64", "android")
	seedProjectTotals(t, db, "p1", 101, 70, 31)
	store := Store{DB: db}

	projects, err := store.TopProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("热门项目排行应合并为 1 条，got=%d: %+v", len(projects), projects)
	}
	item := projects[0]
	if item.ProjectID != "p1" || item.ProjectName != "项目一" ||
		item.DownloadCount != 101 || item.WebDownloadCount != 70 || item.APIDownloadCount != 31 {
		t.Fatalf("热门项目排行结果错误：%+v", item)
	}
}

func TestTopProjectsIncludesEnabledProjectsWithZeroDownloads(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db, "p1", "项目一", true)
	seedRankProject(t, db, "p2", "项目二", true)
	seedProjectTotals(t, db, "p1", 5, 3, 2)
	store := Store{DB: db}

	projects, err := store.TopProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("应返回全部启用项目，got=%d: %+v", len(projects), projects)
	}
	if projects[0].ProjectID != "p1" || projects[0].DownloadCount != 5 {
		t.Fatalf("第一名错误：%+v", projects[0])
	}
	if projects[1].ProjectID != "p2" || projects[1].DownloadCount != 0 {
		t.Fatalf("零下载项目应保留在榜单中：%+v", projects[1])
	}
}

func TestTopProjectsOrdersTiesByNameAndID(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db, "p2", "Alpha", true)
	seedRankProject(t, db, "p1", "Alpha", true)
	seedRankProject(t, db, "p3", "Beta", true)
	seedRankProject(t, db, "p4", "已禁用", false)
	seedProjectTotals(t, db, "p1", 5, 4, 1)
	seedProjectTotals(t, db, "p2", 5, 2, 3)
	seedProjectTotals(t, db, "p3", 5, 1, 4)
	seedProjectTotals(t, db, "p4", 99, 99, 0)
	store := Store{DB: db}

	projects, err := store.TopProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 3 {
		t.Fatalf("禁用项目不应进入公开榜单，got=%d: %+v", len(projects), projects)
	}
	if projects[0].ProjectID != "p1" || projects[1].ProjectID != "p2" || projects[2].ProjectID != "p3" {
		t.Fatalf("同下载量项目排序错误：%+v", projects)
	}
}

func seedRankProject(t *testing.T, db execDB, id, name string, enabled bool) {
	t.Helper()
	enabledValue := 0
	if enabled {
		enabledValue = 1
	}
	_, err := db.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, ?, 2, 0, 1, 'hash', 'now')`,
		id, name, "owner/"+id, enabledValue)
	if err != nil {
		t.Fatal(err)
	}
}

func seedRankReleaseAsset(t *testing.T, db execDB, projectID, releaseID string, githubReleaseID int,
	assetID string, githubAssetID int, fileName, architecture, system string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES (?, ?, ?, '1.0.0', 0, '2026-06-01T00:00:00Z', 1, 'now')`,
		releaseID, projectID, githubReleaseID)
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
}

func seedProjectTotals(t *testing.T, db execDB, projectID string, downloads, web, api int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO project_stat_totals
		(project_id, authorization_count, web_authorization_count, api_authorization_count,
		transfer_started_count, sent_bytes, updated_at)
		VALUES (?, ?, ?, ?, 0, 0, 'now')`,
		projectID, downloads, web, api)
	if err != nil {
		t.Fatal(err)
	}
}

type execDB interface {
	Exec(query string, args ...any) (sql.Result, error)
}
