package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSEOIndexNowDefaultsUseFyhubApexDomain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, MasterExample, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMaster(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.PublicBaseURL != "https://fyhub.cn" || cfg.IndexNow.Enabled == nil || !*cfg.IndexNow.Enabled {
		t.Fatalf("SEO defaults mismatch: server=%q indexnow=%+v", cfg.Server.PublicBaseURL, cfg.IndexNow)
	}
	if cfg.IndexNow.Endpoint != "https://api.indexnow.org/indexnow" || cfg.IndexNow.KeyFile != "secrets/indexnow.key" || cfg.IndexNow.StateFile != "secrets/indexnow.state" {
		t.Fatalf("IndexNow defaults mismatch: %+v", cfg.IndexNow)
	}
}

func TestSEOIndexNowRejectsWwwHostname(t *testing.T) {
	cfg := Master{Server: MasterServer{PublicBaseURL: "https://www.fyhub.cn"}}
	err := validateSEOIndexNow(cfg)
	if err == nil || !strings.Contains(err.Error(), "www.fyhub.cn") {
		t.Fatalf("www hostname should be rejected: %v", err)
	}
}
