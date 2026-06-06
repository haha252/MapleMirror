package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
)

func TestEnsureMasterMaterialsUsesConfiguredCAKey(t *testing.T) {
	cfg := testMasterConfig(t)

	if err := EnsureMasterMaterials(cfg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		cfg.Node.TLS.CAFile,
		cfg.Node.TLS.CAKeyFile,
		cfg.Node.TLS.CertFile,
		cfg.Node.TLS.KeyFile,
		cfg.Admin.TLS.CertFile,
		cfg.Admin.TLS.KeyFile,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated material %s: %v", path, err)
		}
	}
}

func TestMasterNeedsMaterialsChecksConfiguredCAKey(t *testing.T) {
	cfg := testMasterConfig(t)
	for _, path := range []string{
		cfg.DownloadToken.SigningPrivateKeyFile,
		cfg.DownloadToken.VerifyPublicKeyFile,
		cfg.Node.TLS.CAFile,
		cfg.Node.TLS.CertFile,
		cfg.Node.TLS.KeyFile,
		cfg.Node.TLS.SigningCACertFile,
		cfg.Node.TLS.SigningCAKeyFile,
		cfg.Admin.TLS.CertFile,
		cfg.Admin.TLS.KeyFile,
	} {
		writeTestMaterial(t, path)
	}
	if !MasterNeedsMaterials(cfg) {
		t.Fatal("missing configured master CA key should require bootstrap")
	}
	writeTestMaterial(t, cfg.Node.TLS.CAKeyFile)
	if MasterNeedsMaterials(cfg) {
		t.Fatal("all configured materials exist; bootstrap should not be required")
	}
}

func testMasterConfig(t *testing.T) config.Master {
	t.Helper()
	dir := t.TempDir()
	return config.Master{
		DownloadToken: config.DownloadToken{
			SigningPrivateKeyFile: filepath.Join(dir, "tokens", "download.key"),
			VerifyPublicKeyFile:   filepath.Join(dir, "tokens", "download.pub"),
		},
		Node: config.NodeControl{TLS: config.TLS{
			CAFile:            filepath.Join(dir, "custom-ca", "master-ca.pem"),
			CAKeyFile:         filepath.Join(dir, "custom-ca", "master-ca.key"),
			CertFile:          filepath.Join(dir, "tls", "master-control.crt"),
			KeyFile:           filepath.Join(dir, "tls", "master-control.key"),
			SigningCACertFile: filepath.Join(dir, "signing", "node-ca.pem"),
			SigningCAKeyFile:  filepath.Join(dir, "signing", "node-ca.key"),
		}},
		Admin: config.Administration{TLS: config.AdminTLS{
			CertFile: filepath.Join(dir, "admin", "admin.crt"),
			KeyFile:  filepath.Join(dir, "admin", "admin.key"),
		}},
	}
}

func writeTestMaterial(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("material"), 0o600); err != nil {
		t.Fatal(err)
	}
}
