package adminui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"
)

func TestProjectIconAddsCSPForSVG(t *testing.T) {
	server, _ := newTestServer(t)
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "icon.svg")
	data := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`)
	if err := os.WriteFile(iconPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	server.projects = mirrorsync.NewProjectLoader("", config.Projects{Projects: []config.Project{{
		ID:                 "p1",
		Name:               "项目一",
		Repository:         "owner/repo",
		Enabled:            true,
		RetainVersions:     1,
		DownloadMultiplier: 1,
		ResolvedIconPath:   iconPath,
	}}})
	req := httptest.NewRequest(http.MethodGet, "/static/project-icons/p1", nil)
	rec := httptest.NewRecorder()
	server.projectIcon(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
		t.Fatalf("unexpected content type: %s", got)
	}
	assertSVGIconCSP(t, rec.Header().Get("Content-Security-Policy"))
}

func assertSVGIconCSP(t *testing.T, value string) {
	t.Helper()
	for _, want := range []string{"sandbox", "script-src 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(value, want) {
			t.Fatalf("missing svg icon CSP %q in %q", want, value)
		}
	}
}
