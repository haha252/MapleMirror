package public

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestBlocklistRejectsChallengeAndCountsAttempts(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	policy := newBlocklistPolicy(config.Quota{
		Blocklist: config.Blocklist{Static: []string{"192.0.2.0/24"}},
	}, nil)
	server := Server{Store: Store{DB: db}, Blocklist: policy}

	for i := 0; i < 2; i++ {
		body := strings.NewReader(`{"asset_id":"asset-1"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges", body)
		req.RemoteAddr = "192.0.2.9:12345"
		rec := httptest.NewRecorder()
		server.apiChallenge(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("黑名单客户端应被拒绝：code=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	policy.mu.Lock()
	attempts := policy.attempts["192.0.2.9/32"]
	policy.mu.Unlock()
	if attempts != 2 {
		t.Fatalf("封禁后尝试次数应累计：%d", attempts)
	}
}

func TestBlocklistExemptionOverridesStaticBlock(t *testing.T) {
	policy := newBlocklistPolicy(config.Quota{
		Blocklist:  config.Blocklist{Static: []string{"192.0.2.0/24"}},
		Exemptions: []string{"192.0.2.9"},
	}, nil)
	if decision := policy.check("192.0.2.9/32"); decision.Blocked {
		t.Fatalf("白名单地址不应被黑名单拦截：%+v", decision)
	}
}

func TestParseBlocklistFeed(t *testing.T) {
	entries := parseBlocklistFeed(strings.NewReader("192.0.2.1\n192.0.2.1/32\n# comment\n2001:db8::/32\nbad\n"), "feed")
	if len(entries) != 2 {
		t.Fatalf("订阅源应只保留有效 IP/CIDR：%+v", entries)
	}
}

func TestAutoBlockPersistsAndExpires(t *testing.T) {
	db := openMaster(t)
	store := Store{DB: db}
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	if _, err := store.AutoBlockClient(context.Background(), "192.0.2.9/32",
		"request_quota_exhausted", "local_auto_ban", now, time.Hour); err != nil {
		t.Fatal(err)
	}
	decision, err := store.ActiveAutoBlock(context.Background(), "192.0.2.9/32", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Blocked || decision.Attempts != 1 || decision.Reason != "request_quota_exhausted" {
		t.Fatalf("自动封禁状态错误：%+v", decision)
	}
	decision, err = store.ActiveAutoBlock(context.Background(), "192.0.2.9/32", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Blocked {
		t.Fatalf("过期自动封禁不应继续生效：%+v", decision)
	}
}

func TestQuotaErrorWritesAutoBlock(t *testing.T) {
	db := openMaster(t)
	server := Server{
		Store:     Store{DB: db},
		Blocklist: newBlocklistPolicy(config.Quota{}, nil),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/authorizations", nil)
	server.autoBlockAfterQuotaError(req, "192.0.2.9/32", "asset-1", errTrafficLimit)
	decision, err := server.Store.ActiveAutoBlock(context.Background(), "192.0.2.9/32", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Blocked || decision.Reason != "traffic_limit_exceeded" {
		t.Fatalf("额度错误应写入自动封禁：%+v", decision)
	}
}
