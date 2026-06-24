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
		{Level: "warn", Message: "备案已经完成，我们正在执行迁移！最近一段时间，服务质量将会有所下降，部分时间段内会不可用！"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `<title>枫源镜像</title>`) {
		t.Fatalf("expected mirror title in page: %s", body)
	}
	if !strings.Contains(body, `class="site-brand__primary">枫源</span>`) ||
		!strings.Contains(body, `class="site-brand__secondary">镜像</span>`) {
		t.Fatalf("expected split brand text in page: %s", body)
	}
	if !strings.Contains(body, `<meta name="description" content="枫源镜像 是一个公益镜像服务，面向 Github Release 设计。我们致力于为所有用户提供高速且稳定的下载服务，获取到软件的最新版本。">`) {
		t.Fatalf("expected mirror description meta in page: %s", body)
	}
	first := strings.Index(body, `page-notice page-notice--info`)
	second := strings.Index(body, `page-notice page-notice--warn`)
	if first < 0 || second < 0 || first >= second || !strings.Contains(body, `第一条公告`) ||
		!strings.Contains(body, `备案已经完成，我们正在执行迁移！最近一段时间，服务质量将会有所下降，部分时间段内会不可用！`) {
		t.Fatalf("expected test notice in page: %s", body)
	}
	if !strings.Contains(body, `class="project-card panel-card"`) {
		t.Fatalf("expected project card in page: %s", body)
	}
	if !strings.Contains(body, `class="version-select"`) || !strings.Contains(body, `class="architecture-select"`) {
		t.Fatalf("expected selectors in page: %s", body)
	}
	if !strings.Contains(body, `/static/public/download.js?v=`) || !strings.Contains(body, `id="palette-toggle"`) {
		t.Fatalf("expected themed assets in page: %s", body)
	}
	if !strings.Contains(body, `/static/public/download-selectors.js?v=`) {
		t.Fatalf("expected selector helper in page: %s", body)
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
		"p1": {SystemMatchEnabled: true},
	}}

	body := string(catalogBody(t, srv))
	if !strings.Contains(body, `"system_match_enabled":true`) ||
		!strings.Contains(body, `"system":"win"`) {
		t.Fatalf("expected system selector and system payload: %s", body)
	}
}

func TestDownloadPageIncludesArchitectureMatchFlagWhenEnabled(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, ProjectAssets: map[string]projectAssetConfig{
		"p1": {ArchitectureMatchEnabled: true},
	}}

	body := string(catalogBody(t, srv))
	if !strings.Contains(body, `"architecture_match_enabled":true`) {
		t.Fatalf("expected architecture match flag: %s", body)
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

func TestAPIDocsPageOnlyDocumentsPublicAPI(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodGet, "/api-docs", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	if !strings.Contains(body, `<title>API 文档 - 枫源镜像</title>`) {
		t.Fatalf("expected API docs browser title in page: %s", body)
	}
	for _, want := range []string{
		`<body class="page-api-docs">`,
		`方式一：跳转主站验证页下载`,
		`方式二：程序调用 API 下载`,
		`其他接口`,
		`href="#web-download-flow"`,
		`href="#api-download-flow"`,
		`href="#other-public-apis"`,
		`主站地址，不是下载节点地址`,
		`/api/public/v1/projects`,
		`/api/public/v1/projects/{project_id}/assets`,
		`/api/public/v1/api/challenges`,
		`/api/public/v1/api/authorizations`,
		`/api/public/v1/catalog`,
		`ETag`,
		`If-None-Match`,
		`304 Not Modified`,
		`/api/public/v1/blocklist.txt`,
		`/api/public/v1/blocklist.json`,
		`/{project_id}/{version}/{file_name}`,
		`download_url`,
		`Authorization: Bearer &lt;download_token&gt;`,
		`class="api-method method-get"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected API docs page to include %q: %s", want, body)
		}
	}
	if strings.Contains(body, "/api/admin/v1") {
		t.Fatalf("API docs page must not document admin API: %s", body)
	}
	home := strings.Index(body, `href="/"`)
	stats := strings.Index(body, `href="/stats"`)
	docs := strings.Index(body, `href="/api-docs"`)
	about := strings.Index(body, `href="/about"`)
	if home < 0 || stats < 0 || docs < 0 || about < 0 || !(home < stats && stats < docs && docs < about) {
		t.Fatalf("expected nav order home, stats, API docs, about: %s", body)
	}
}

func TestAPIDocsPageRejectsNonGETWithoutPageView(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodPost, "/api-docs", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("expected JSON method error, body=%s", rec.Body.String())
	}
	var rows int
	err := db.QueryRow(`SELECT COUNT(*) FROM daily_site_stats`).Scan(&rows)
	if err != nil || rows != 0 {
		t.Fatalf("non-GET /api-docs should not count page view: rows=%d err=%v", rows, err)
	}
}
