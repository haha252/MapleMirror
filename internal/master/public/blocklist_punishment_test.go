package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestBlockedAttemptLogSampling(t *testing.T) {
	wants := map[int64]bool{1: true, 2: true, 3: false, 4: true, 5: false, 8: true, 9: false}
	for attempts, want := range wants {
		if got := shouldLogAttempt(attempts); got != want {
			t.Fatalf("shouldLogAttempt(%d)=%v want %v", attempts, got, want)
		}
	}
}

func TestStaticAndFeedExactBlocksCanTriggerPunishment(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	quota := config.Quota{AbuseControl: cfg, Blocklist: config.Blocklist{
		Static: []string{"192.0.2.9/32"},
	}}
	staticPolicy := newBlocklistPolicy(quota, nil)
	var decision blockDecision
	for i := 0; i < cfg.Punishment.BurstAttempts; i++ {
		decision = staticPolicy.check("192.0.2.9/32")
	}
	if !decision.PunishmentActive {
		t.Fatalf("静态精确封禁短时达到 %d 次后应触发惩罚：%+v",
			cfg.Punishment.BurstAttempts, decision)
	}

	feedPolicy := newBlocklistPolicy(config.Quota{AbuseControl: cfg}, nil)
	feedPolicy.feedItems["https://feed.example.test/list.txt"] = []blocklistEntry{{
		prefix: mustBlockPrefix(t, "198.51.100.8/32"), source: "198.51.100.8/32",
	}}
	for i := 0; i < cfg.Punishment.BurstAttempts; i++ {
		decision = feedPolicy.check("198.51.100.8/32")
	}
	if !decision.PunishmentActive {
		t.Fatalf("订阅精确封禁短时达到 %d 次后应触发惩罚：%+v",
			cfg.Punishment.BurstAttempts, decision)
	}
}

func TestStaticIPv4Slash24NeverTriggersPunishment(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	policy := newBlocklistPolicy(config.Quota{
		AbuseControl: cfg,
		Blocklist:    config.Blocklist{Static: []string{"192.0.2.0/24"}},
	}, nil)
	var decision blockDecision
	for i := 0; i < 1000; i++ {
		decision = policy.check("192.0.2.9/32")
	}
	if decision.PunishmentActive {
		t.Fatalf("IPv4 /24 无论次数多少都不应触发惩罚：%+v", decision)
	}
}

func TestExactFeedMatchOverridesStaticSlash24(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	policy := newBlocklistPolicy(config.Quota{
		AbuseControl: cfg,
		Blocklist:    config.Blocklist{Static: []string{"192.0.2.0/24"}},
	}, nil)
	policy.feedItems["https://feed.example.test/list.txt"] = []blocklistEntry{{
		prefix: mustBlockPrefix(t, "192.0.2.9/32"), source: "192.0.2.9/32",
	}}
	var decision blockDecision
	for i := 0; i < cfg.Punishment.BurstAttempts; i++ {
		decision = policy.check("192.0.2.9/32")
	}
	if decision.Key != "192.0.2.9/32" || !decision.PunishmentActive {
		t.Fatalf("精确订阅条目应优先于静态 /24：%+v", decision)
	}
}

func TestStaticExactBlockRendersPunishmentPage(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	policy := newBlocklistPolicy(config.Quota{
		AbuseControl: cfg,
		Blocklist:    config.Blocklist{Static: []string{"192.0.2.9/32"}},
	}, nil)
	for i := 1; i < cfg.Punishment.BurstAttempts; i++ {
		policy.check("192.0.2.9/32")
	}
	srv := Server{Blocklist: policy, AbuseTracker: newAbuseTracker(cfg)}
	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	if !srv.rejectBlockedDownload(rec, req, "asset-1", "download_page") {
		t.Fatal("静态精确封禁请求应被拒绝")
	}
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "punishment-pow.js") {
		t.Fatalf("静态精确封禁达到阈值后应显示惩罚页：code=%d body=%s", rec.Code, rec.Body.String())
	}
}
