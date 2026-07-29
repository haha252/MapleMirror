package adminui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestWriteProjectsFilePreservesProjectTags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.yaml")
	projects := config.Projects{Projects: []config.Project{{
		ID: "demo", Name: "演示", Repository: "owner/demo",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
		Tags: map[string][]string{
			"software_type":   {"launcher"},
			"private_keyword": {"alpha"},
		},
	}}}
	if err := writeProjectsFile(path, projects); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadProjects(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Projects[0].Tags["private_keyword"]; len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("管理面板保存后标签丢失：%+v", loaded.Projects[0].Tags)
	}
}

func TestAdminScriptsCarryTagsWithoutAddingEditor(t *testing.T) {
	for _, name := range []string{"project-edit.js", "projects.js"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "admin", "static", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !containsAll(text, `Tags: val(p, "Tags", {}) || {}`) {
			t.Fatalf("%s 应透明保留 Tags", name)
		}
		if containsAll(text, `data-field="Tags"`) {
			t.Fatalf("%s 不应增加标签编辑控件", name)
		}
	}
}

func containsAll(value string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(value, want) {
			return false
		}
	}
	return true
}
