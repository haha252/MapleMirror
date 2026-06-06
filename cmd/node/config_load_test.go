package main

import (
	"path/filepath"
	"testing"
)

func TestLoadNodeConfigStopsAfterExampleCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	cfg, created, err := loadNodeConfig(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("missing node config should report example creation")
	}
	if cfg.Server.Listen != "" {
		t.Fatalf("generated example must not be loaded as active config: %+v", cfg.Server)
	}
}
