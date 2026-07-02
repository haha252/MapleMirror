package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMasterLoadsScanSocks5Config(t *testing.T) {
	text := strings.Replace(string(MasterExample), `    password: ""`,
		`    password: "secret"`, 1)
	text = strings.Replace(text, `    username: ""`, `    username: "mirror"`, 1)
	text = strings.Replace(text, "    enabled: false", "    enabled: true", 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadMaster(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Scan.Socks5.Enabled || c.Scan.Socks5.URL != "socks5h://127.0.0.1:1080" ||
		c.Scan.Socks5.Username != "mirror" || c.Scan.Socks5.Password != "secret" {
		t.Fatalf("socks5 config mismatch: %+v", c.Scan.Socks5)
	}
}

func TestMasterRejectsInvalidEnabledScanSocks5(t *testing.T) {
	cases := []string{
		`url: "http://127.0.0.1:1080"`,
		`url: "socks5://user:pass@127.0.0.1:1080"`,
		`url: ""`,
	}
	for _, replacement := range cases {
		t.Run(replacement, func(t *testing.T) {
			text := strings.Replace(string(MasterExample), "    enabled: false", "    enabled: true", 1)
			text = strings.Replace(text, `    url: "socks5h://127.0.0.1:1080"`, "    "+replacement, 1)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadMaster(path, nil); err == nil {
				t.Fatalf("should reject %s", replacement)
			}
		})
	}
}
