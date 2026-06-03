package admin

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
)

func TestAuthReadsTokenFileAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "admin-token")
	if err := os.WriteFile(tokenFile, []byte("abcdefghijklmnopqrstuvwxyz123456\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := NewAuth(config.Administration{
		AllowedCIDRs: []string{"127.0.0.0/8"}, TokenFile: tokenFile, TokenMinBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz123456")
	if _, ok := auth.Check(req, false); !ok {
		t.Fatal("令牌文件中的管理令牌应通过鉴权")
	}

	t.Setenv("MIRROR_TEST_ADMIN_TOKEN", "override-token-abcdefghijklmnopqrstuvwxyz")
	auth, err = NewAuth(config.Administration{
		AllowedCIDRs: []string{"127.0.0.0/8"}, TokenEnv: "MIRROR_TEST_ADMIN_TOKEN",
		TokenFile: tokenFile, TokenMinBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer override-token-abcdefghijklmnopqrstuvwxyz")
	if _, ok := auth.Check(req, false); !ok {
		t.Fatal("环境变量管理令牌应优先于文件")
	}
}

func TestAuthAllowsLoopbackHighRiskWithoutMTLS(t *testing.T) {
	t.Setenv("MIRROR_TEST_ADMIN_TOKEN", "abcdefghijklmnopqrstuvwxyz123456")
	auth, err := NewAuth(config.Administration{
		AllowedCIDRs:  []string{"127.0.0.0/8", "::1/128"},
		TokenEnv:      "MIRROR_TEST_ADMIN_TOKEN",
		TokenMinBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz123456")
	if _, ok := auth.Check(req, true); !ok {
		t.Fatal("回环地址的高风险管理请求应允许在无 mTLS 时通过")
	}
}

func TestAuthRejectsNonLoopbackHighRiskWithoutMTLS(t *testing.T) {
	t.Setenv("MIRROR_TEST_ADMIN_TOKEN", "abcdefghijklmnopqrstuvwxyz123456")
	auth, err := NewAuth(config.Administration{
		AllowedCIDRs:  []string{"10.0.0.0/8"},
		TokenEnv:      "MIRROR_TEST_ADMIN_TOKEN",
		TokenMinBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz123456")
	if _, ok := auth.Check(req, true); ok {
		t.Fatal("非回环地址的高风险管理请求仍应要求 mTLS")
	}
}
