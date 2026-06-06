package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureServerCertRejectsInvalidCAPEM(t *testing.T) {
	cases := []struct {
		name    string
		damage  func(string, string) error
		wantErr string
	}{
		{
			name: "cert",
			damage: func(certPath, _ string) error {
				return os.WriteFile(certPath, []byte("not pem"), 0o600)
			},
			wantErr: "CA 证书 PEM 内容无效",
		},
		{
			name: "key",
			damage: func(_, keyPath string) error {
				return os.WriteFile(keyPath, []byte("not pem"), 0o600)
			},
			wantErr: "CA 私钥 PEM 内容无效",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			caCert := filepath.Join(dir, "ca.crt")
			caKey := filepath.Join(dir, "ca.key")
			if err := EnsureCA(caCert, caKey, "test-ca"); err != nil {
				t.Fatal(err)
			}
			if err := tc.damage(caCert, caKey); err != nil {
				t.Fatal(err)
			}
			err := EnsureServerCert(filepath.Join(dir, "server.crt"),
				filepath.Join(dir, "server.key"), caCert, caKey, "server", nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}
