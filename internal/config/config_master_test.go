package config

import (
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
	if c.Logging.MaxFileSizeMB != DefaultLogMaxFileSizeMB {
		t.Fatalf("日志文件大小上限 = %d MiB，want %d MiB",
			c.Logging.MaxFileSizeMB, DefaultLogMaxFileSizeMB)
	}
	if c.Server.EnrollmentListen == "" || c.Node.HeartbeatInterval != "15s" ||
		c.Node.HeartbeatTimeout != "90s" || c.Node.HeartbeatOfflineGrace != "5m" ||
		c.Node.TLS.CAKeyFile == "" || c.Admin.Web.UsersFile == "" ||
		c.Scan.GitHubTimeout != "2m" {
		t.Fatal("控制面配置默认值缺失")
	}
}

func TestLoggingRejectsInvalidMaxFileSize(t *testing.T) {
	cfg := Logging{
		ConsoleLevel:  "info",
		FileLevel:     "info",
		Directory:     "logs",
		RetentionDays: 30,
		MaxFileSizeMB: -1,
	}
	if err := validateLogging(cfg); err == nil || !strings.Contains(err.Error(), "logging.max_file_size_mb") {
		t.Fatalf("应拒绝非法日志大小上限：%v", err)
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
