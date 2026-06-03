package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsLoadArchitectureDefaultEnabled(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    architecture_regex: '(amd64)'\n    architecture_default_enabled: true\n"
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !projects.Projects[0].ArchitectureDefaultEnabled {
		t.Fatal("架构兜底开关未正确加载")
	}
}

func TestProjectsValidateArchitectureRegexWhenEnabled(t *testing.T) {
	cases := []string{
		"architecture_match_enabled: true\n",
		"architecture_match_enabled: true\n    architecture_regex: '('\n",
	}
	for _, extra := range cases {
		t.Run(extra, func(t *testing.T) {
			dir := t.TempDir()
			projectsPath := filepath.Join(dir, "projects.yaml")
			body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    " + extra
			if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProjects(projectsPath, nil); err == nil {
				t.Fatalf("启用架构匹配时应校验 architecture_regex：%s", extra)
			}
		})
	}
}

func TestProjectsAllowsMissingArchitectureRegexWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n"
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if projects.Projects[0].ArchitectureMatchEnabled {
		t.Fatal("未配置 architecture_match_enabled 时不应启用架构匹配")
	}
}
