package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeParsesPeerFallbackMaxConcurrent(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "node.yaml")
	body := strings.Replace(string(NodeExample),
		`peer_fallback_max_concurrent: 3`,
		`peer_fallback_max_concurrent: 6`, 1)
	if err := os.WriteFile(nodePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadNode(nodePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sync.PeerFallbackMaxConcurrent != 6 {
		t.Fatalf("peer fallback max concurrent=%d, want 6", cfg.Sync.PeerFallbackMaxConcurrent)
	}
}

func TestNodeRejectsInvalidPeerFallbackMaxConcurrent(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "node.yaml")
	body := strings.Replace(string(NodeExample),
		`peer_fallback_max_concurrent: 3`,
		`peer_fallback_max_concurrent: -1`, 1)
	if err := os.WriteFile(nodePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(nodePath, nil); err == nil ||
		!strings.Contains(err.Error(), "节点间复制全局并发数") {
		t.Fatalf("expected peer fallback max concurrent error, got %v", err)
	}
}
