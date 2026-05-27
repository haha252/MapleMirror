package controltls

import (
	"crypto/tls"
	"testing"
)

func TestTLS13TemplateIsStrict(t *testing.T) {
	cfg := tls13()
	if cfg.MinVersion != tls.VersionTLS13 || cfg.MaxVersion != tls.VersionTLS13 {
		t.Fatal("控制面 TLS 必须固定为 TLS 1.3")
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	got := NormalizeFingerprint(" AA BB ")
	if got != "sha256:aabb" {
		t.Fatalf("证书指纹规范化错误：%s", got)
	}
}
