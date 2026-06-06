package adminui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"
)

func TestWriteProjectsFileUsesUniqueTemporaryFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.yaml")
	for _, id := range []string{"demo-a", "demo-b"} {
		projects := config.Projects{Projects: []config.Project{{
			ID: id, Name: "演示项目", Repository: "owner/demo",
			Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
		}}}
		if err := writeProjectsFile(path, projects); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := config.LoadProjects(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects[0].ID != "demo-b" {
		t.Fatalf("latest projects file not written, got %q", loaded.Projects[0].ID)
	}
	assertNoProjectTempFiles(t, filepath.Dir(path))
}

func TestWriteProjectsFileCleansInvalidTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")
	projects := config.Projects{Projects: []config.Project{{
		ID: "bad", Name: "坏项目", Repository: "owner/bad",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
		SystemMatchEnabled: true,
	}}}
	if err := writeProjectsFile(path, projects); err == nil {
		t.Fatal("invalid project config should be rejected")
	}
	assertNoProjectTempFiles(t, dir)
}

func TestSaveProjectsDeletesAndDisablesProjects(t *testing.T) {
	server, db := newTestServer(t)
	path := filepath.Join(t.TempDir(), "projects.yaml")
	initial := config.Projects{Projects: []config.Project{
		{ID: "keep", Name: "保留", Repository: "owner/keep", Enabled: true, RetainVersions: 1, DownloadMultiplier: 1},
		{ID: "drop", Name: "删除", Repository: "owner/drop", Enabled: true, RetainVersions: 1, DownloadMultiplier: 1},
	}}
	if err := writeProjectsFile(path, initial); err != nil {
		t.Fatal(err)
	}
	server.projects = mirrorsync.NewProjectLoader(path, initial)
	if err := server.syncStore.SyncProjectConfig(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	next := config.Projects{Projects: []config.Project{
		{ID: "keep", Name: "保留", Repository: "owner/keep", Enabled: false, RetainVersions: 1, DownloadMultiplier: 1},
	}}
	body, _ := json.Marshal(next)
	req := httptest.NewRequest(http.MethodPut, "/admin/api/projects", strings.NewReader(string(body)))
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.saveProjects(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM project_scan_state WHERE project_id = 'keep'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("keep enabled=%d, want disabled", enabled)
	}
	if err := db.QueryRow(`SELECT enabled FROM project_scan_state WHERE project_id = 'drop'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("drop enabled=%d, want disabled", enabled)
	}
}

func assertNoProjectTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".projects-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("project temp files should be cleaned: %+v", matches)
	}
	if _, err := os.Stat(filepath.Join(dir, ".projects.tmp")); err == nil {
		t.Fatal("legacy fixed temp file should not be created")
	}
}
