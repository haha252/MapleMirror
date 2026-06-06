package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeRejectsUnsafePublicDownloadBaseURL(t *testing.T) {
	for _, raw := range []string{
		"http://node.example.com",
		"https://node.example.com/prefix",
		"https://node.example.com?token=leak",
	} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "node.yaml")
			body := strings.Replace(string(NodeExample),
				`public_download_base_url: "https://node1.example.com"`,
				`public_download_base_url: "`+raw+`"`, 1)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadNode(path, nil); err == nil {
				t.Fatal("unsafe public_download_base_url should be rejected")
			}
		})
	}
}

func TestNodeAllowsLoopbackHTTPPublicDownloadBaseURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	body := strings.Replace(string(NodeExample),
		`public_download_base_url: "https://node1.example.com"`,
		`public_download_base_url: "http://127.0.0.1:8081"`, 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNode(path, nil); err != nil {
		t.Fatalf("loopback HTTP public_download_base_url should be allowed: %v", err)
	}
}
