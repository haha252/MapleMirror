package public

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if !strings.Contains(body, `"asset_id":"asset-1"`) {
		t.Fatalf("expected download data payload in page: %s", body)
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
	for _, want := range []string{
		`<body class="page-api-docs">`,
		`/api/public/v1/projects`,
		`/api/public/v1/api/challenges`,
		`/api/public/v1/api/authorizations`,
		`/downloads/{asset_id}`,
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

func TestProjectIconServesConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "icon.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := Server{ProjectAssets: map[string]projectAssetConfig{
		"p1": {IconPath: iconPath},
	}}
	req := httptest.NewRequest(http.MethodGet, "/static/project-icons/p1", nil)
	rec := httptest.NewRecorder()
	srv.projectIcon(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
		t.Fatalf("unexpected content type: %s", got)
	}
}

func TestProjectIconFallsBackToPlaceholderWhenFileMissing(t *testing.T) {
	srv := Server{ProjectAssets: map[string]projectAssetConfig{
		"p1": {IconPath: filepath.Join(t.TempDir(), "missing.svg")},
	}}
	req := httptest.NewRequest(http.MethodGet, "/static/project-icons/p1", nil)
	rec := httptest.NewRecorder()
	srv.projectIcon(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "项目占位图标") {
		t.Fatalf("expected placeholder icon body: %s", rec.Body.String())
	}
}

func TestProjectIconRejectsUnknownProject(t *testing.T) {
	srv := Server{ProjectAssets: map[string]projectAssetConfig{}}
	req := httptest.NewRequest(http.MethodGet, "/static/project-icons/unknown", nil)
	rec := httptest.NewRecorder()
	srv.projectIcon(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
