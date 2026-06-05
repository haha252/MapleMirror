package adminui

import (
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
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
