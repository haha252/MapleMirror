package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuotaValidatesBlocklist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.yaml")
	body := []byte(`
blocklist:
  static:
    - 192.0.2.0/24
  feeds:
    - url: "https://example.com/all.txt"
      refresh_interval: "1h"
      timeout: "5s"
  auto_ban_duration: "336h"
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	quota, err := LoadQuota(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(quota.Blocklist.Static) != 1 || len(quota.Blocklist.Feeds) != 1 {
		t.Fatalf("黑名单配置未加载：%+v", quota.Blocklist)
	}
	if quota.Blocklist.AutoBanDuration != "336h" {
		t.Fatalf("自动封禁时长未加载：%q", quota.Blocklist.AutoBanDuration)
	}
}

func TestQuotaDefaultsPublicResourceBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	quota, err := LoadQuota(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if quota.PublicResourceBuckets.IPv432.Capacity != 3600 ||
		quota.PublicResourceBuckets.IPv424.Capacity != 10800 ||
		quota.PublicResourceBuckets.IPv6128.Capacity != 3600 ||
		quota.PublicResourceBuckets.IPv664.Capacity != 10800 {
		t.Fatalf("公共资源限流默认值错误：%+v", quota.PublicResourceBuckets)
	}
	for _, value := range []string{
		quota.PublicResourceBuckets.IPv432.FullRefill,
		quota.PublicResourceBuckets.IPv424.FullRefill,
		quota.PublicResourceBuckets.IPv6128.FullRefill,
		quota.PublicResourceBuckets.IPv664.FullRefill,
	} {
		if value != "1h" {
			t.Fatalf("公共资源限流补充周期=%q，期望 1h", value)
		}
	}
}

func TestQuotaRejectsInvalidBlocklistFeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.yaml")
	body := []byte("blocklist:\n  feeds:\n    - url: \"file:///tmp/list.txt\"\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuota(path, nil); err == nil {
		t.Fatal("黑名单订阅源必须拒绝非 http/https URL")
	}
}

func TestQuotaRejectsNonIncreasingEscalationDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quota.yaml")
	body := []byte(`
abuse_control:
  blocked:
    escalation:
      level_1_duration: "720h"
      level_2_duration: "700h"
      level_3_duration: "8760h"
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuota(path, nil); err == nil {
		t.Fatal("封禁升级期限必须严格递增")
	}
}
