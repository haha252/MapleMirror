package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDefaultSelectionModeNormalization(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"", ProjectSelectionModeSelectors},
		{" selectors ", ProjectSelectionModeSelectors},
		{" FILE ", ProjectSelectionModeFile},
	}
	for _, tc := range cases {
		project := Project{DefaultSelectionMode: tc.value}
		if got := project.NormalizedDefaultSelectionMode(); got != tc.want {
			t.Fatalf("NormalizedDefaultSelectionMode(%q)=%q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestLoadSplitProjectKeepsDefaultSelectionMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "projects.yaml"),
		[]byte("project_files:\n  - projects/*.yaml\n"))
	projectPath := filepath.Join(dir, "projects", "demo.yaml")
	writeTestFile(t, projectPath, []byte(`id: demo
name: 演示项目
repository: owner/demo
enabled: true
retain_versions: 1
download_multiplier: 1
default_selection_mode: FILE
`))

	projects, err := LoadProjects(filepath.Join(dir, "projects.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := projects.Projects[0].NormalizedDefaultSelectionMode(); got != ProjectSelectionModeFile {
		t.Fatalf("默认选择模式=%q，期望 %q", got, ProjectSelectionModeFile)
	}
	if content := readTestFile(t, projectPath); !strings.Contains(content, "default_selection_mode: FILE") {
		t.Fatalf("拆分项目配置不应裁剪默认选择模式：%s", content)
	}
}

func TestLoadProjectsRejectsInvalidDefaultSelectionMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.yaml")
	writeTestFile(t, path, []byte(`projects:
  - id: demo
    name: 演示项目
    repository: owner/demo
    enabled: true
    retain_versions: 1
    download_multiplier: 1
    default_selection_mode: invalid
`))

	_, err := LoadProjects(path, nil)
	if err == nil || !strings.Contains(err.Error(), "default_selection_mode 必须为 selectors 或 file") {
		t.Fatalf("非法默认选择模式应被拒绝，err=%v", err)
	}
}
