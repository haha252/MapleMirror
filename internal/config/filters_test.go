package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFiltersCreatesExampleAndParsesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.yaml")
	if _, err := LoadFilters(path, nil); !errors.Is(err, ErrExampleCreated) {
		t.Fatalf("缺失筛选配置应生成示例：%v", err)
	}
	filters, err := LoadFilters(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filters.CacheBytes != 8*1024*1024 || len(filters.Selectors) != 2 ||
		filters.Selectors[0].ID != "software_type" {
		t.Fatalf("筛选配置默认合同错误：%+v", filters)
	}
}

func TestLoadFiltersAllowsZeroByteCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.yaml")
	body := "cache:\n  max_size: 0 B\nselectors: []\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	filters, err := LoadFilters(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filters.CacheBytes != 0 {
		t.Fatalf("0 B 应关闭查询缓存，got %d", filters.CacheBytes)
	}
}

func TestLoadFiltersRejectsInvalidOrDuplicateIDs(t *testing.T) {
	cases := []string{
		"selectors:\n  - id: Bad\n    name: 类型\n",
		"selectors:\n  - id: type\n    name: 类型\n  - id: type\n    name: 重复\n",
		"selectors:\n  - id: type\n    name: 类型\n    options:\n      - id: same\n        name: 一\n      - id: same\n        name: 二\n",
	}
	for _, body := range cases {
		t.Run(strings.ReplaceAll(body, "\n", "_"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "filters.yaml")
			content := "cache:\n  max_size: 8 MiB\n" + body
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFilters(path, nil); err == nil {
				t.Fatalf("应拒绝非法筛选配置：\n%s", content)
			}
		})
	}
}

func TestProjectTagsAllowUndeclaredGroupsAndSurviveRepair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.yaml")
	projectDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("project_files:\n  - projects/*.yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "id: demo\nname: 演示\nrepository: owner/demo\nenabled: true\n" +
		"tags:\n  keywords: [ffmpeg, video]\n"
	projectPath := filepath.Join(projectDir, "demo.yaml")
	if err := os.WriteFile(projectPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := projects.Projects[0].Tags["keywords"]; len(got) != 2 || got[0] != "ffmpeg" {
		t.Fatalf("未声明标签未保留：%+v", projects.Projects[0].Tags)
	}
	repaired, err := os.ReadFile(projectPath)
	if err != nil || !strings.Contains(string(repaired), "keywords:") {
		t.Fatalf("项目配置修复不应删除 tags：err=%v body=%s", err, repaired)
	}
}

func TestProjectTagsRejectEmptyAndDuplicateValues(t *testing.T) {
	for _, tags := range []map[string][]string{
		{"": {"value"}},
		{"type": {""}},
		{"type": {"Tool", "tool"}},
	} {
		project := Project{ID: "demo", Name: "演示", Repository: "owner/demo",
			Enabled: true, RetainVersions: 1, DownloadMultiplier: 1, Tags: tags}
		if err := validateProject(project, map[string]bool{}); err == nil {
			t.Fatalf("应拒绝非法项目标签：%+v", tags)
		}
	}
}
