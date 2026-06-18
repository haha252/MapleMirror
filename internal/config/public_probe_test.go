package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMasterRejectsInvalidPublicProbeConfig(t *testing.T) {
	text := strings.Replace(string(MasterExample),
		`public_probe_ttl: "30s"`, `public_probe_ttl: "4s"`, 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte(text), 0o600)
	if _, err := LoadMaster(path, nil); err == nil {
		t.Fatal("node.public_probe_ttl should be greater than public_probe_timeout")
	}
}
