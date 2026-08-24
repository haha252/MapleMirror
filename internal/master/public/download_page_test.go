package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestDownloadPageIncludesButtonForAvailableAsset(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, Notices: []config.PublicNotice{
		{Level: "info", Message: "第一条公告"},
		{Level: "warn", Message: "这是一个示例公告，请根据实际情况修改。"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `<title>枫源镜像 - GitHub Release 软件版本与文件下载服务</title>`) {
		t.Fatalf("expected mirror title in page: %s", body)
	}
	if !strings.Contains(body, `class="site-brand__primary">枫源</span>`) ||
		!strings.Contains(body, `class="site-brand__secondary">镜像</span>`) {
		t.Fatalf("expected split brand text in page: %s", body)
	}
	if !strings.Contains(body, `<meta name="description" content="枫源镜像是面向 GitHub Release 的公益镜像下载服务，提供免费、稳定、快速的软件版本与文件下载，支持项目搜索、版本筛选、镜像节点状态查看、网页验证下载和公共 API 接入，适用于网页用户、脚本工具与自动更新器。">`) {
		t.Fatalf("expected mirror description meta in page: %s", body)
	}
	first := strings.Index(body, `page-notice page-notice--info`)
	second := strings.Index(body, `page-notice page-notice--warn`)
	if first < 0 || second < 0 || first >= second || !strings.Contains(body, `第一条公告`) ||
		!strings.Contains(body, `这是一个示例公告，请根据实际情况修改。`) {
		t.Fatalf("expected test notice in page: %s", body)
	}
	filters, mobileSearch := strings.Index(body, `id="catalog-filters"`), strings.Index(body, `id="catalog-search-mobile"`)
	if filters < 0 || mobileSearch < 0 || first >= filters || filters >= mobileSearch ||
		strings.Count(body, `第一条公告`) != 1 {
		t.Fatalf("公告应在左侧筛选器上方，移动搜索应排在公告之后：%s", body)
	}
	if !strings.Contains(body, `class="project-card panel-card"`) {
		t.Fatalf("expected project card in page: %s", body)
	}
	if !strings.Contains(body, `class="version-select"`) ||
		!strings.Contains(body, `class="architecture-select"`) ||
		!strings.Contains(body, `class="file-browser"`) ||
		!strings.Contains(body, `data-folder-icon=`) ||
		!strings.Contains(body, `class="file-browser__level file-browser__level--versions"`) ||
		!strings.Contains(body, `class="file-browser__level file-browser__level--files" hidden`) ||
		!strings.Contains(body, `class="file-browser__back"`) ||
		!strings.Contains(body, `class="file-browser__versions" role="group"`) ||
		!strings.Contains(body, `class="file-browser__files" role="group"`) ||
		!strings.Contains(body, `data-selection-mode="selectors"`) ||
		!strings.Contains(body, `data-selection-mode="file"`) ||
		!strings.Contains(body, `#selection-conditions`) ||
		!strings.Contains(body, `#selection-files`) {
		t.Fatalf("expected selectors in page: %s", body)
	}
	assertDownloadCardLayout(t, body)
	if !strings.Contains(body, `/static/public/download.js?v=`) || !strings.Contains(body, `id="palette-toggle"`) {
		t.Fatalf("expected themed assets in page: %s", body)
	}
	if !strings.Contains(body, `/static/public/download-selectors.js?v=`) {
		t.Fatalf("expected selector helper in page: %s", body)
	}
	if !strings.Contains(body, `/static/public/download-file-browser.js?v=`) {
		t.Fatalf("expected file browser helper in page: %s", body)
	}
	if strings.Contains(body, `<script src="/static/public/pow-loader.js`) || strings.Contains(body, `challenge-overlay`) {
		t.Fatalf("home page should link to standalone download verification page: %s", body)
	}
	if strings.Contains(body, "architecture_default_enabled") {
		t.Fatalf("download payload must not expose deprecated architecture default field: %s", body)
	}
	if strings.Contains(body, `"asset_id":"asset-1"`) || strings.Contains(body, `"default_version":"v1"`) {
		t.Fatalf("home page should not inline catalog payload: %s", body)
	}

	catalog := catalogBody(t, srv)
	catalogText := string(catalog)
	for _, want := range []string{
		`/static/project-icons/p1`,
		`"system_match_enabled":false`,
		`"architecture_match_enabled":false`,
		`"system_selector_enabled":false`,
		`"architecture_selector_enabled":false`,
		`"default_selection_mode":"selectors"`,
		`"default_version":"v1"`,
		`"asset_id":"asset-1"`,
	} {
		if !strings.Contains(catalogText, want) {
			t.Fatalf("expected catalog to include %q: %s", want, catalogText)
		}
	}
}

func TestDownloadPageDefaultsToLatestVersion(t *testing.T) {
	db := openMaster(t)
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-old', 'p1', 1, 'v1.0.0', 0, '2026-01-01T00:00:00Z', 1, '2026-01-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-new', 'p1', 2, 'v2.0.0', 0, '2026-02-01T00:00:00Z', 1, '2026-02-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-old', 'rel-old', 1, 'old.zip', 'amd64', 12,
		'https://example.test/old.zip', 'sha256:old', 'candidate', '2026-01-01T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-new', 'rel-new', 2, 'new.zip', 'amd64', 12,
		'https://example.test/new.zip', 'sha256:new', 'candidate', '2026-02-01T00:00:00Z')`)
	srv := Server{Store: Store{DB: db}}

	body := string(catalogBody(t, srv))
	if !strings.Contains(body, `"default_version":"v2.0.0"`) {
		t.Fatalf("expected latest version to be selected by default: %s", body)
	}
}

func TestDownloadPageIncludesSystemSelectorWhenEnabled(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE assets SET system = 'win' WHERE id = 'asset-1'`)
	srv := Server{Store: Store{DB: db}, ProjectAssets: map[string]projectAssetConfig{
		"p1": {SystemSelectorEnabled: true},
	}}

	body := string(catalogBody(t, srv))
	if !strings.Contains(body, `"system_match_enabled":true`) ||
		!strings.Contains(body, `"system_selector_enabled":true`) ||
		!strings.Contains(body, `"system":"win"`) {
		t.Fatalf("expected system selector and system payload: %s", body)
	}
}

func TestDownloadPageIncludesArchitectureMatchFlagWhenEnabled(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, ProjectAssets: map[string]projectAssetConfig{
		"p1": {ArchitectureSelectorEnabled: true},
	}}

	body := string(catalogBody(t, srv))
	if !strings.Contains(body, `"architecture_match_enabled":true`) ||
		!strings.Contains(body, `"architecture_selector_enabled":true`) {
		t.Fatalf("expected architecture match flag: %s", body)
	}
}

func TestProjectAssetMapUsesExplicitSelectorSwitches(t *testing.T) {
	enabled := true
	disabled := false
	items := projectAssetMap(config.Projects{Projects: []config.Project{{
		ID: "p1",
		AssetPipeline: config.AssetPipeline{
			Selectors: config.AssetSelectorConfig{
				ArchitectureEnabled: &disabled,
				SystemEnabled:       &enabled,
			},
			Classify: config.AssetClassifyConfig{
				Mode: "rules",
				Rules: []config.AssetClassifyRule{{
					Match:  config.AssetClassifyMatch{Exact: "tool.zip"},
					Assign: config.AssetClassification{Architecture: "amd64"},
				}},
			},
		},
	}}})
	if items["p1"].ArchitectureSelectorEnabled || !items["p1"].SystemSelectorEnabled ||
		items["p1"].DefaultSelectionMode != config.ProjectSelectionModeSelectors {
		t.Fatalf("公开页选择器开关未按项目显式配置生效：%+v", items["p1"])
	}
}

func catalogBody(t *testing.T, srv Server) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.catalog(rec, httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("catalog should be JSON: %v body=%s", err, rec.Body.String())
	}
	if _, ok := body["projects"]; !ok {
		t.Fatalf("catalog should contain projects: %s", rec.Body.String())
	}
	return rec.Body.Bytes()
}

func TestProjectAssetsAPIIncludesSystemField(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE assets SET system = 'linux' WHERE id = 'asset-1'`)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/projects/p1/assets", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"system":"linux"`) {
		t.Fatalf("expected system field in API response, code=%d body=%s", rec.Code, body)
	}
}

func TestDownloadPageDoesNotHandleUnknownPath(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodGet, "/favicon-missing.ico", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown path, got %d", rec.Code)
	}
	var views int
	err := db.QueryRow(`SELECT COUNT(*) FROM daily_site_stats`).Scan(&views)
	if err != nil || views != 0 {
		t.Fatalf("未知路径不应计入访问量：views=%d err=%v", views, err)
	}
}

func TestHandlerIgnoresFaviconForPageViews(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}, WebAssets: &webAssets{staticDir: t.TempDir()}}
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for favicon, got %d", rec.Code)
	}
	var rows int
	err := db.QueryRow(`SELECT COUNT(*) FROM daily_site_stats`).Scan(&rows)
	if err != nil || rows != 0 {
		t.Fatalf("favicon 不应计入访问量：rows=%d err=%v", rows, err)
	}
}
