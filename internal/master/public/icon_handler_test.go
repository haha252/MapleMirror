package public

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectIconServesConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "icon.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`), 0o600); err != nil {
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
	assertIconCSP(t, rec.Header().Get("Content-Security-Policy"))
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

func assertIconCSP(t *testing.T, value string) {
	t.Helper()
	for _, want := range []string{"sandbox", "script-src 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(value, want) {
			t.Fatalf("missing svg icon CSP %q in %q", want, value)
		}
	}
}
