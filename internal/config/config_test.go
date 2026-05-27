package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMasterExample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, MasterExample, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadMaster(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.APIPoW.Algorithm != "sha256" || c.APIPoW.LeadingZeroBits != 23 {
		t.Fatal("公开 API PoW 默认合同被修改")
	}
	if c.ALTCHA.WorkloadProfile != "medium" || c.Admin.HighRiskRequireMTLS == nil || !*c.Admin.HighRiskRequireMTLS {
		t.Fatal("验证或管理安全合同被修改")
	}
}

func TestMasterRejectsPublicManagementListen(t *testing.T) {
	text := strings.Replace(string(MasterExample), "127.0.0.1:9080", "0.0.0.0:9080", 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte(text), 0o600)
	if _, err := LoadMaster(path, nil); err == nil {
		t.Fatal("管理监听不得对公网开放")
	}
}

func TestMissingConfigurationWritesChineseExample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	_, err := LoadNode(path, nil)
	if !errors.Is(err, ErrExampleCreated) {
		t.Fatalf("应生成配置并安全退出，实际错误：%v", err)
	}
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "下载节点配置") {
		t.Fatal("生成的配置缺少中文注释")
	}
}

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
	quota, err := LoadQuota(quotaPath, nil)
	if err != nil || quota.RequestBuckets.IPv6128.Capacity != 120 || quota.DailyTraffic.IPv664 != "20 GiB" {
		t.Fatalf("额度默认合同错误：%v", err)
	}
}

func TestWriteExamplesKeepsEmbeddedUTF8Content(t *testing.T) {
	dir := t.TempDir()
	if err := WriteExamples(dir); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "config.example.yaml"))
	if err != nil || string(content) != string(MasterExample) {
		t.Fatal("导出的主节点中文示例与嵌入模板不一致")
	}
}
