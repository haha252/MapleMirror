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
	if c.ALTCHA.Difficulty != 22 {
		t.Fatal("网页验证安全合同被修改")
	}
	if c.Server.EnrollmentListen == "" || c.Node.HeartbeatInterval != "10s" ||
		c.Node.HeartbeatTimeout != "60s" || c.Node.HeartbeatOfflineGrace != "60s" ||
		c.Node.TLS.CAKeyFile == "" || c.Admin.Web.UsersFile == "" {
		t.Fatal("控制面配置默认值缺失")
	}
}

func TestMasterAllowsExplicitManagementNetworkListenForWebPanel(t *testing.T) {
	text := strings.Replace(string(MasterExample), "127.0.0.1:9080", "0.0.0.0:9080", 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte(text), 0o600)
	if _, err := LoadMaster(path, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMasterPublicProbeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, MasterExample, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadMaster(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Node.PublicProbeEnabled == nil || !*c.Node.PublicProbeEnabled ||
		c.Node.PublicProbeInterval != "60s" ||
		c.Node.PublicProbeTimeout != "10s" ||
		c.Node.PublicProbeTTL != "30s" ||
		c.Node.PublicProbeNetworkFailures != 5 {
		t.Fatalf("public probe defaults mismatch: %+v", c.Node)
	}
}

func TestMasterRejectsInvalidPublicProbeConfig(t *testing.T) {
	text := strings.Replace(string(MasterExample),
		`public_probe_ttl: "30s"`, `public_probe_ttl: "4s"`, 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte(text), 0o600)
	if _, err := LoadMaster(path, nil); err == nil {
		t.Fatal("node.public_probe_ttl should be greater than public_probe_timeout")
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

func TestMissingConfigurationCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configs", "node.yaml")
	_, err := LoadNode(path, nil)
	if !errors.Is(err, ErrExampleCreated) {
		t.Fatalf("应生成嵌套目录中的配置示例，实际错误：%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
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

func TestSaveNodeFirstRunPersistsInteractiveAnswers(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "node.yaml")
	if err := os.WriteFile(nodePath, NodeExample, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadNode(nodePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Node.Name = "华东下载节点"
	cfg.Master.ControlAddress = "https://162.14.72.24:10001"
	cfg.Master.EnrollmentAddress = "https://162.14.72.24:10002"
	cfg.TLS.ServerName = "master.example.com"
	if err := SaveNodeFirstRun(nodePath, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadNode(nodePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Node.Name != cfg.Node.Name ||
		loaded.Master.ControlAddress != cfg.Master.ControlAddress ||
		loaded.Master.EnrollmentAddress != cfg.Master.EnrollmentAddress ||
		loaded.TLS.ServerName != cfg.TLS.ServerName {
		t.Fatalf("首次交互配置未持久化：%+v", loaded)
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
