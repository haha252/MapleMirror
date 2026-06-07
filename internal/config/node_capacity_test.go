package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNodeDefaultsMaxMirrorProjectsToUnlimited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	body := strings.Replace(string(NodeExample), "  max_mirror_projects: 0\n", "", 1)
	writeTestFile(t, path, []byte(body))
	cfg, err := LoadNode(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sync.MaxMirrorProjects != 0 {
		t.Fatalf("max_mirror_projects=%d want 0", cfg.Sync.MaxMirrorProjects)
	}
}

func TestLoadNodeRejectsNegativeMaxMirrorProjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	body := strings.Replace(string(NodeExample), "max_mirror_projects: 0", "max_mirror_projects: -1", 1)
	writeTestFile(t, path, []byte(body))
	if _, err := LoadNode(path, nil); err == nil {
		t.Fatal("expected negative max_mirror_projects to fail")
	}
}
