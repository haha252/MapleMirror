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
	if c.Server.EnrollmentListen == "" || c.Node.HeartbeatInterval != "10s" || c.Admin.TokenMinBytes != 32 {
		t.Fatal("M2 控制面配置默认值缺失")
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

func TestNodeRejectsInvalidTrustedProxyCIDR(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "node.yaml")
	body := []byte(`node:
  name: "节点一"
server:
  listen: ":8081"
  public_download_base_url: "https://node1.example.com"
master:
  control_address: "https://127.0.0.1:9443"
storage:
  directory: "data/assets"
  temp_directory: "data/tmp"
  state_db: "data/node-state.db"
bandwidth:
  target: "100 MiB/s"
sync:
  max_workers: 1
download_token:
  verify_public_key_file: "data/download-token-ed25519.pub"
proxy:
  trusted_cidrs: ["bad-cidr"]
tls:
  server_name: "127.0.0.1"
`)
	if err := os.WriteFile(nodePath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(nodePath, nil); err == nil {
		t.Fatal("下载节点应拒绝无效 proxy.trusted_cidrs")
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
