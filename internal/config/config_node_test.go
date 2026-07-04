package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
