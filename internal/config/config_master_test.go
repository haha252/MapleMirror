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

func TestMasterCatalogRowsDefaultsAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := strings.Replace(string(MasterExample), "  catalog_batch_rows: 4\n", "", 1)
	legacy = strings.Replace(legacy, "  catalog_prefetch_remaining_rows: 1\n", "", 1)
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadMaster(path, nil)
	if err != nil || c.Server.CatalogBatchRows == nil ||
		*c.Server.CatalogBatchRows != 4 ||
		c.Server.CatalogPrefetchRemainingRows == nil ||
		*c.Server.CatalogPrefetchRemainingRows != 1 {
		t.Fatalf("目录行数默认值错误：config=%+v err=%v", c.Server, err)
	}
	repaired, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(repaired), "catalog_batch_rows: 4") ||
		!strings.Contains(string(repaired), "catalog_prefetch_remaining_rows: 1") {
		t.Fatalf("旧配置未自动补齐目录行数字段：content=%s err=%v", repaired, err)
	}

	custom := strings.Replace(string(MasterExample),
		"catalog_batch_rows: 4", "catalog_batch_rows: 6", 1)
	custom = strings.Replace(custom,
		"catalog_prefetch_remaining_rows: 1", "catalog_prefetch_remaining_rows: 2", 1)
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err = LoadMaster(path, nil)
	if err != nil || *c.Server.CatalogBatchRows != 6 ||
		*c.Server.CatalogPrefetchRemainingRows != 2 {
		t.Fatalf("目录行数自定义值错误：config=%+v err=%v", c.Server, err)
	}

	invalidCases := []string{
		strings.Replace(string(MasterExample),
			"catalog_batch_rows: 4", "catalog_batch_rows: 0", 1),
		strings.Replace(string(MasterExample),
			"catalog_prefetch_remaining_rows: 1",
			"catalog_prefetch_remaining_rows: 4", 1),
	}
	for _, invalid := range invalidCases {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadMaster(path, nil); err == nil {
			t.Fatal("应拒绝非法目录行数配置")
		}
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
