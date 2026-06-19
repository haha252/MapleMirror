package adminui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"

	"gopkg.in/yaml.v3"
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

func TestWriteSplitProjectsFileAddsUpdatesAndDeletesProjectFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(path, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestProjectFile(t, filepath.Join(projectDir, "keep.yaml"), config.Project{
		ID: "keep", Name: "旧名称", Repository: "owner/keep",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	})
	writeTestProjectFile(t, filepath.Join(projectDir, "drop.yaml"), config.Project{
		ID: "drop", Name: "删除", Repository: "owner/drop",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	})
	next := config.Projects{Projects: []config.Project{
		{ID: "keep", Name: "新名称", Repository: "owner/keep", Enabled: false, RetainVersions: 2, DownloadMultiplier: 1},
		{ID: "new", Name: "新增", Repository: "owner/new", Enabled: true, RetainVersions: 1, DownloadMultiplier: 1},
	}}
	if err := writeProjectsFile(path, next); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadProjects(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 2 || loaded.Projects[0].ID != "keep" || loaded.Projects[1].ID != "new" {
		t.Fatalf("拆分项目保存结果错误：%+v", loaded.Projects)
	}
	if loaded.Projects[0].Name != "新名称" || loaded.Projects[0].Enabled {
		t.Fatalf("已有项目文件未更新：%+v", loaded.Projects[0])
	}
	if _, err := os.Stat(filepath.Join(projectDir, "new.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "drop.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("删除项目文件应被移除，err=%v", err)
	}
	entry, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(entry), "id:") || !strings.Contains(string(entry), "projects/*.yaml") {
		t.Fatalf("拆分模式入口不应写回完整项目：%s", entry)
	}
	assertNoProjectTempFiles(t, dir)
}

func TestWriteSplitProjectsFileUpdatesExplicitProjectFileList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")
	body := "project_files:\n  - projects/keep.yaml\n  - projects/drop.yaml\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestProjectFile(t, filepath.Join(projectDir, "keep.yaml"), config.Project{
		ID: "keep", Name: "保留", Repository: "owner/keep",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	})
	writeTestProjectFile(t, filepath.Join(projectDir, "drop.yaml"), config.Project{
		ID: "drop", Name: "删除", Repository: "owner/drop",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	})
	next := config.Projects{Projects: []config.Project{
		{ID: "keep", Name: "保留", Repository: "owner/keep", Enabled: true, RetainVersions: 1, DownloadMultiplier: 1},
		{ID: "new", Name: "新增", Repository: "owner/new", Enabled: true, RetainVersions: 1, DownloadMultiplier: 1},
	}}
	if err := writeProjectsFile(path, next); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(entry)
	if strings.Contains(text, "projects/drop.yaml") || !strings.Contains(text, "projects/new.yaml") {
		t.Fatalf("显式 project_files 未正确更新：%s", text)
	}
}

func TestWriteSplitProjectsFileRejectsUnsafeNewProjectID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(path, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projects := config.Projects{Projects: []config.Project{{
		ID: "bad/id", Name: "坏项目", Repository: "owner/bad",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	}}}
	if err := writeProjectsFile(path, projects); err == nil {
		t.Fatal("不安全的新项目 ID 应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "bad", "id.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("非法项目不应写入文件，err=%v", err)
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
	stages, err := filepath.Glob(filepath.Join(dir, ".projects-stage-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 0 {
		t.Fatalf("project stage directories should be cleaned: %+v", stages)
	}
}

func writeTestProjectFile(t *testing.T, path string, project config.Project) {
	t.Helper()
	data, err := yaml.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
