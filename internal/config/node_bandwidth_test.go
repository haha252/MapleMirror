package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNodeParsesBandwidthFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	body := strings.Replace(string(NodeExample), `bandwidth_limit: "0"`,
		`bandwidth_limit: "80 Mbps"`, 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadNode(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bandwidth.TargetBPS != 100*1024*1024 {
		t.Fatalf("target bandwidth parsed incorrectly: %d", cfg.Bandwidth.TargetBPS)
	}
	if cfg.Sync.BandwidthLimitBPS != 10*1000*1000 {
		t.Fatalf("sync bandwidth limit parsed incorrectly: %d", cfg.Sync.BandwidthLimitBPS)
	}
}

func TestLoadNodeRejectsInvalidBandwidthLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	body := strings.Replace(string(NodeExample), `bandwidth_limit: "0"`,
		`bandwidth_limit: "fast"`, 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadNode(path, nil)
	if err == nil || !strings.Contains(err.Error(), "配置字段 sync.bandwidth_limit") {
		t.Fatalf("expected Chinese sync.bandwidth_limit error, got %v", err)
	}
}

func TestLoadNodeMigratesDeprecatedIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	body := strings.Replace(string(NodeExample), `name: "示例下载节点"`,
		"name: \"示例下载节点\"\n  id_file: \"data/node-id\"", 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	if _, err := LoadNode(path, func(field, value string) {
		if replacement, ok := DeprecatedWarningMessage(value); ok {
			warnings = append(warnings, field+"=>"+replacement)
		}
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "id_file") {
		t.Fatalf("deprecated node.id_file should be removed: %s", content)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "node.id_file=>") {
		t.Fatalf("deprecated node.id_file warning missing: %+v", warnings)
	}
}
