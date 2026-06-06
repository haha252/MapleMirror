package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProjectsMigratesArchitectureDefaultEnabled(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    architecture_regex: '(amd64)'\n    architecture_default_enabled: true\n"
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	projects, err := LoadProjects(projectsPath, func(field, value string) {
		if replacement, ok := DeprecatedWarningMessage(value); ok {
			warnings = append(warnings, field+"=>"+replacement)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !projects.Projects[0].ArchitectureMatchEnabled {
		t.Fatal("旧 architecture_default_enabled 应自动迁移为 architecture_match_enabled")
	}
	if !containsString(warnings, "projects[].architecture_default_enabled=>projects[].architecture_match_enabled") {
		t.Fatalf("旧架构字段应产生弃用警告：%+v", warnings)
	}
	content := readTestFile(t, projectsPath)
	if strings.Contains(content, "architecture_default_enabled") ||
		!strings.Contains(content, "architecture_match_enabled: true") {
		t.Fatalf("旧架构字段应写回迁移为新字段：%s", content)
	}
	encoded, err := yaml.Marshal(projects)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "architecture_default_enabled") {
		t.Fatalf("保存项目配置时不应主动写出旧字段：%s", encoded)
	}
}

func TestProjectsMigrationDoesNotOverrideExistingArchitectureMatch(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    architecture_default_enabled: true\n    architecture_match_enabled: false\n"
	writeTestFile(t, projectsPath, []byte(body))
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if projects.Projects[0].ArchitectureMatchEnabled {
		t.Fatal("已有 architecture_match_enabled 时旧字段不应覆盖新字段")
	}
	content := readTestFile(t, projectsPath)
	if strings.Contains(content, "architecture_default_enabled") {
		t.Fatalf("已有新字段时也应删除旧字段：%s", content)
	}
}

func TestProjectsValidateArchitectureRegexWhenEnabled(t *testing.T) {
	cases := []string{
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

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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
