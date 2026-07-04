package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsAndQuotaDefaults(t *testing.T) {
	dir := t.TempDir()
	projectsPath := filepath.Join(dir, "projects.yaml")
	quotaPath := filepath.Join(dir, "quota.yaml")
	_ = os.WriteFile(projectsPath, []byte("projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    architecture_regex: '(amd64)'\n"), 0o600)
	_ = os.WriteFile(quotaPath, []byte("{}\n"), 0o600)
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil || projects.Projects[0].RetainVersions != 3 || projects.Projects[0].DownloadMultiplier != 1 {
		t.Fatalf("项目默认合同错误：%v", err)
	}
	if projects.Projects[0].ArchitectureMatchEnabled || projects.Projects[0].SystemMatchEnabled {
		t.Fatal("项目默认不应启用架构匹配或系统匹配")
	}
	quota, err := LoadQuota(quotaPath, nil)
	if err != nil || quota.RequestBuckets.IPv6128.Capacity != 120 ||
		quota.DailyTraffic.IPv664 != "20 GiB" ||
		quota.AuthorizationMaxBytesMultiplier != 2 ||
		quota.RangeConcurrencyLimit != 32 ||
		quota.Blocklist.AutoBanDuration != "168h" {
		t.Fatalf("额度默认合同错误：%v", err)
	}
}

func TestProjectsValidateSystemRegexWhenEnabled(t *testing.T) {
	cases := []string{
		"system_match_enabled: true\n    system_regex: '('\n",
	}
	for _, extra := range cases {
		t.Run(extra, func(t *testing.T) {
			dir := t.TempDir()
			projectsPath := filepath.Join(dir, "projects.yaml")
			body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    architecture_regex: '(amd64)'\n    " + extra
			if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProjects(projectsPath, nil); err == nil {
				t.Fatalf("启用系统匹配时应校验 system_regex：%s", extra)
			}
		})
	}
}

func TestProjectsResolveIconPathRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	iconDir := filepath.Join(dir, "project-icons")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectsPath := filepath.Join(dir, "projects.yaml")
	body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    icon_path: project-icons/a.svg\n    architecture_regex: '(amd64)'\n"
	if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := projects.Projects[0].ResolvedIconPath; got != filepath.Join(iconDir, "a.svg") {
		t.Fatalf("icon_path 解析错误：%q", got)
	}
}

func TestProjectsRejectInvalidIconPath(t *testing.T) {
	cases := []string{
		"icon_path: C:/tmp/a.svg",
		"icon_path: C:\\tmp\\a.svg",
		"icon_path: \\\\server\\share\\a.svg",
		"icon_path: ../a.svg",
		"icon_path: project-icons/a.txt",
	}
	for _, line := range cases {
		t.Run(line, func(t *testing.T) {
			dir := t.TempDir()
			projectsPath := filepath.Join(dir, "projects.yaml")
			body := "projects:\n  - id: a\n    name: 示例\n    repository: owner/repo\n    enabled: true\n    " + line + "\n    architecture_regex: '(amd64)'\n"
			if err := os.WriteFile(projectsPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProjects(projectsPath, nil); err == nil {
				t.Fatalf("应拒绝非法 icon_path：%s", line)
			}
		})
	}
}
