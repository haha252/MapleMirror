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
	if !strings.Contains(body, `/static/public/download.js`) || !strings.Contains(body, `id="theme-toggle"`) {
		t.Fatalf("expected themed assets in page: %s", body)
	}
	if !strings.Contains(body, `"asset_id":"asset-1"`) {
		t.Fatalf("expected download data payload in page: %s", body)
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
