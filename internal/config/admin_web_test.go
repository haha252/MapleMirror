package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMasterRejectsPlainAdminWebWithoutExclusiveSessionMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := strings.Replace(string(MasterExample), "https_enabled: true", "https_enabled: false", 1)
	body = strings.Replace(body, "enabled: false", "enabled: true", 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMaster(path, nil); err == nil ||
		!strings.Contains(err.Error(), "admin.web.https_enabled=false") {
		t.Fatalf("应拒绝非专用会话模式关闭管理 HTTPS，实际：%v", err)
	}
}

func TestMasterAllowsPlainAdminWebBehindExclusiveSessionProxy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := strings.Replace(string(MasterExample), "https_enabled: true", "https_enabled: false", 1)
	body = strings.Replace(body, "enabled: false", "enabled: true", 1)
	body = strings.Replace(body, "exclusive_api: false", "exclusive_api: true", 1)
	body = strings.Replace(body, "high_risk_session_allowed: false", "high_risk_session_allowed: true", 1)
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
