package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVDFMigrationAndExplicitV1Disable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := strings.Replace(string(MasterExample), "vdf:\n", "vdf:\n", 1)
	legacy = strings.Replace(legacy, "  challenge_ttl: \"2m\"\n", "", 1)
	legacy = strings.Replace(legacy, "# 公开 API PoW 挑战配置。\n",
		"altcha:\n  challenge_ttl: \"3m\"\n\n# 公开 API PoW 挑战配置。\n", 1)
	legacy = strings.Replace(legacy, "  v1_enabled: true", "  v1_enabled: false", 1)
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	var deprecated bool
	cfg, err := LoadMaster(path, func(field, value string) {
		if field == "altcha.challenge_ttl" {
			_, deprecated = DeprecatedWarningMessage(value)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VDF.ChallengeTTL != "3m" || cfg.APIPoW.V1Enabled == nil || *cfg.APIPoW.V1Enabled {
		t.Fatalf("migration result=%+v v1=%v", cfg.VDF, cfg.APIPoW.V1Enabled)
	}
	if !deprecated {
		t.Fatal("legacy TTL deprecation warning missing")
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "altcha:") || !strings.Contains(string(content), "challenge_ttl: \"3m\"") {
		t.Fatalf("legacy TTL was not rewritten: %s", content)
	}
}

func TestVDFValidationRejectsInvalidBounds(t *testing.T) {
	cfg := Master{VDFSizeTiers: []VDFSizeTier{{MinSize: "0 B", Iterations: 1}},
		VDF: VDF{ChallengeTTL: "2m", KeyRotationInterval: "1m", ElevatedMultiplier: 2,
			SevereMultiplier: 4, MaxIterations: 10, MaxParallelCreations: 1}}
	if err := validateVDF(cfg); err == nil || !strings.Contains(err.Error(), "key_rotation_interval") {
		t.Fatalf("rotation bound error=%v", err)
	}
	cfg.VDF.KeyRotationInterval = "24h"
	cfg.VDFSizeTiers[0].Iterations = VDFIterationsHardLimit + 1
	if err := validateVDF(cfg); err == nil || !strings.Contains(err.Error(), "iterations") {
		t.Fatalf("iteration bound error=%v", err)
	}
}
