package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMasterAllowsPlainAdminWebBehindProxy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := strings.Replace(string(MasterExample), "https_enabled: true", "https_enabled: false", 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMaster(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Admin.Web.HTTPSEnabled == nil || *cfg.Admin.Web.HTTPSEnabled {
		t.Fatal("https_enabled 应保持 false")
	}
}
