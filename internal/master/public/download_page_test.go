package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadPageIncludesButtonForAvailableAsset(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `<title>枫源镜像</title>`) {
		t.Fatalf("expected mirror title in page: %s", body)
	}
	if !strings.Contains(body, `<meta name="description" content="枫源镜像 是一个公益镜像服务，面向 Github Release 设计。我们致力于为所有用户提供高速且稳定的下载服务，获取到软件的最新版本。">`) {
		t.Fatalf("expected mirror description meta in page: %s", body)
	}
	if !strings.Contains(body, `<div class="page-notice" role="status" aria-live="polite">本站目前处于测试状态，会出现不稳定，不可用的情况。预计将在六月中旬进入完全稳定的生产状态。</div>`) {
		t.Fatalf("expected test notice in page: %s", body)
	}
	if !strings.Contains(body, `class="project-card panel-card"`) {
		t.Fatalf("expected project card in page: %s", body)
	}
	if !strings.Contains(body, `class="version-select"`) || !strings.Contains(body, `class="architecture-select"`) {
		t.Fatalf("expected selectors in page: %s", body)
	}
	if !strings.Contains(body, `/static/project-icons/p1`) {
		t.Fatalf("expected project icon route in page: %s", body)
	}
	if !strings.Contains(body, `/static/public/download.js`) || !strings.Contains(body, `id="palette-toggle"`) {
		t.Fatalf("expected themed assets in page: %s", body)
	}
	if !strings.Contains(body, `/static/public/download-selectors.js`) {
		t.Fatalf("expected selector helper in page: %s", body)
	}
	if strings.Contains(body, `/static/public/pow-loader.js`) || strings.Contains(body, `challenge-overlay`) {
		t.Fatalf("home page should link to standalone download verification page: %s", body)
	}
	if !strings.Contains(body, `"system_match_enabled":false`) {
		t.Fatalf("expected disabled system matching in payload: %s", body)
	}
	if !strings.Contains(body, `"architecture_match_enabled":false`) {
		t.Fatalf("expected disabled architecture matching in payload: %s", body)
	}
	if !strings.Contains(body, `"architecture_default_enabled":false`) {
		t.Fatalf("expected disabled architecture default in payload: %s", body)
	}
	if !strings.Contains(body, `"default_version":"v1"`) {
		t.Fatalf("expected default version in payload: %s", body)
	}
	if !strings.Contains(body, `"asset_id":"asset-1"`) {
		t.Fatalf("expected download data payload in page: %s", body)
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="system-select"`) ||
		!strings.Contains(body, `"system_match_enabled":true`) ||
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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"architecture_match_enabled":true`) ||
		!strings.Contains(body, `class="field architecture-field" hidden`) {
		t.Fatalf("expected architecture match flag and hidden field template: %s", body)
	}
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
		`/api/public/v1/projects`,
		`/api/public/v1/api/challenges`,
		`/api/public/v1/api/authorizations`,
		`/{project_id}/{version}/{file_name}`,
		`/static/public/api-docs.css`,
		`class="api-method method-get"`,
		`class="api-method method-post"`,
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
