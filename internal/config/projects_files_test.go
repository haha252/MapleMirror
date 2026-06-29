package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsLoadProjectFilesSortedAndResolveRelativeToProjectFile(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(filepath.Join(projectDir, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(entry, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := `id: a
name: 项目 A
repository: owner/a
enabled: true
description: 项目 A 描述
homepage_url: https://a.example.test
icon_path: icons/a.svg
asset_pipeline:
  classify:
    script:
      path: scripts/a.star
`
	b := "id: b\nname: 项目 B\nrepository: owner/b\nenabled: true\n"
	if err := os.WriteFile(filepath.Join(projectDir, "b.yaml"), []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "a.yaml"), []byte(a), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{projects.Projects[0].ID, projects.Projects[1].ID}; got[0] != "a" || got[1] != "b" {
		t.Fatalf("项目文件应按路径排序加载：%+v", got)
	}
	if projects.Projects[0].RetainVersions != 3 || projects.Projects[0].DownloadMultiplier != 1 {
		t.Fatalf("拆分项目默认值未补齐：%+v", projects.Projects[0])
	}
	if projects.Projects[0].Description != "项目 A 描述" ||
		projects.Projects[0].HomepageURL != "https://a.example.test" {
		t.Fatalf("拆分项目页面字段解析错误：%+v", projects.Projects[0])
	}
	if got := projects.Projects[0].ResolvedIconPath; got != filepath.Join(projectDir, "icons", "a.svg") {
		t.Fatalf("拆分项目 icon_path 应相对项目文件目录解析，got %q", got)
	}
	if got := projects.Projects[0].ResolvedClassifyScriptPath; got != filepath.Join(projectDir, "scripts", "a.star") {
		t.Fatalf("拆分项目脚本路径应相对项目文件目录解析，got %q", got)
	}
}

func TestProjectsRejectInvalidHomepageURL(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(entry, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "id: a\nname: 项目 A\nrepository: owner/a\nenabled: true\nhomepage_url: ftp://example.test\n"
	if err := os.WriteFile(filepath.Join(projectDir, "a.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(entry, nil); err == nil {
		t.Fatal("非法 homepage_url 应被拒绝")
	}
}

func TestProjectsRejectDuplicateIDAcrossProjectFiles(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(entry, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "id: same\nname: 重复\nrepository: owner/repo\nenabled: true\n"
	if err := os.WriteFile(filepath.Join(projectDir, "a.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "b.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(entry, nil); err == nil {
		t.Fatal("重复项目 ID 应被拒绝")
	}
}

func TestProjectsRejectInvalidProjectFilePath(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(entry, []byte("project_files:\n  - ../projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(entry, nil); err == nil {
		t.Fatal("project_files 不应允许越级路径")
	}
}

func TestProjectsRejectInvalidRuleInProjectFile(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "projects.yaml")
	if err := os.WriteFile(entry, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "id: a\nname: 项目 A\nrepository: owner/a\nenabled: true\nasset_include:\n  - pattern: '('\n    type: regex\n"
	if err := os.WriteFile(filepath.Join(projectDir, "a.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjects(entry, nil); err == nil {
		t.Fatal("拆分项目文件中的非法规则应被拒绝")
	}
}
