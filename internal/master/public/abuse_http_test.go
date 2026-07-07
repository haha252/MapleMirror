package public

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestAbuseTrackerCleanupRemovesIdleSeries(t *testing.T) {
	tracker := newAbuseTracker(testAbuseControl("enforce", true))
	now := time.Now().UTC()
	tracker.record("web", "192.0.2.9/32", 1, now.Add(-25*time.Hour))
	tracker.cleanup(now)
	if len(tracker.exact) != 0 || len(tracker.network) != 0 {
		t.Fatalf("闲置 tracker 未清理：exact=%d network=%d", len(tracker.exact), len(tracker.network))
	}
}

func TestPunishmentPageReturnsForbiddenHTML(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	srv := Server{AbuseTracker: newAbuseTracker(cfg)}
	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	rec := httptest.NewRecorder()
	srv.renderPunishmentPage(rec, req, blockDecision{Key: "192.0.2.9/32"})
	if rec.Code != http.StatusForbidden || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("惩罚页响应错误：code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"difficulty":128`) || !strings.Contains(body, `"worker_limit":32`) {
		t.Fatalf("惩罚页缺少挑战参数：%s", body)
	}
}

func TestBlockedDownloadPageUsesFastPunishmentPath(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	insertTestClientBlock(t, db, "192.0.2.9/32", "local_auto_ban", now)
	if _, err := db.Exec(`UPDATE client_blocks SET punishment_active = 1
		WHERE client_prefix_key = '192.0.2.9/32'`); err != nil {
		t.Fatal(err)
	}
	cfg := testAbuseControl("enforce", true)
	srv := Server{
		Store: Store{DB: db}, Blocklist: newBlocklistPolicy(config.Quota{}, nil),
		ClientBlocks: newClientBlockManager(db, cfg, nil), AbuseTracker: newAbuseTracker(cfg),
	}
	req := httptest.NewRequest(http.MethodGet, "/download/missing-asset", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	srv.downloadPowPage(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "punishment-pow.js") {
		t.Fatalf("封禁下载页未走惩罚快速路径：code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIChallengeReturnsAdaptiveDifficulty(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	cfg := testAbuseControl("enforce", true)
	tracker := newAbuseTracker(cfg)
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		tracker.record("api", "192.0.2.9/32", 1, now)
	}
	srv := Server{
		Store: Store{DB: db}, APIZeroBits: 23, APITTL: time.Minute,
		Blocklist: newBlocklistPolicy(config.Quota{}, nil), AbuseTracker: tracker,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges",
		strings.NewReader(`{"asset_id":"asset-1"}`))
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	srv.apiChallenge(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建挑战失败：%d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Bits int `json:"leading_zero_bits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Bits != 25 {
		t.Fatalf("动态难度=%d，期望 25", body.Data.Bits)
	}
}

func TestAPIChallengeRejectsRequestThatReachesThreshold(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	tracker := newAbuseTracker(cfg)
	now := time.Now().UTC()
	for i := 0; i < cfg.Challenge.Exact.RejectBurst-1; i++ {
		tracker.record("api", "192.0.2.9/32", 1, now)
	}
	srv := Server{AbuseTracker: tracker, APIZeroBits: 23}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges",
		strings.NewReader(`{"asset_id":"asset-1"}`))
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	srv.apiChallenge(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("达到拒绝阈值的请求状态=%d，期望 429：%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidSolutionWeightUsesConfiguration(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	cfg.Challenge.InvalidSolutionWeight = 7
	srv := Server{AbuseTracker: newAbuseTracker(cfg)}
	if got := srv.invalidSolutionWeight(); got != 7 {
		t.Fatalf("invalid solution weight=%d，期望 7", got)
	}
}

func TestIPv4NetworkInvalidationClearsHostNegativeCache(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	m := newClientBlockManager(db, testAbuseControl("enforce", true), nil)
	if decision, err := m.resolve(context.Background(), "192.0.2.9/32", now); err != nil || decision.Blocked {
		t.Fatalf("初次查询应未封禁：decision=%+v err=%v", decision, err)
	}
	insertTestClientBlock(t, db, "192.0.2.0/24", "manual", now)
	m.invalidate("192.0.2.0/24")
	decision, err := m.resolve(context.Background(), "192.0.2.9/32", now)
	if err != nil || !decision.Blocked {
		t.Fatalf("/24 新增后应立即生效：decision=%+v err=%v", decision, err)
	}
}

func TestExactInvalidationIsNotBypassedByNetworkNegativeCache(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	m := newClientBlockManager(db, testAbuseControl("enforce", true), nil)
	if decision, err := m.resolve(context.Background(), "192.0.2.9/32", now); err != nil || decision.Blocked {
		t.Fatalf("初次查询应未封禁：decision=%+v err=%v", decision, err)
	}
	insertTestClientBlock(t, db, "192.0.2.9/32", "manual", now)
	m.invalidate("192.0.2.9/32")
	decision, err := m.resolve(context.Background(), "192.0.2.9/32", now)
	if err != nil || !decision.Blocked {
		t.Fatalf("精确 IP 新增后应立即生效：decision=%+v err=%v", decision, err)
	}
}

func TestBlockedAttemptLogSampling(t *testing.T) {
	wants := map[int64]bool{1: true, 2: true, 3: false, 4: true, 5: false, 8: true, 9: false}
	for attempts, want := range wants {
		if got := shouldLogAttempt(attempts); got != want {
			t.Fatalf("shouldLogAttempt(%d)=%v want %v", attempts, got, want)
		}
	}
}
