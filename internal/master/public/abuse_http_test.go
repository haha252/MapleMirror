package public

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
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
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	srv.renderPunishmentPage(rec, req, blockDecision{Key: "192.0.2.9/32"})
	if rec.Code != http.StatusForbidden || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("惩罚页响应错误：code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"difficulty":128`) || !strings.Contains(body, `"worker_limit":32`) {
		t.Fatalf("惩罚页缺少挑战参数：%s", body)
	}
	if !strings.Contains(body, `"source":"192.0.2.9"`) || !strings.Contains(body, ">192.0.2.9<") {
		t.Fatalf("惩罚页应显示完整客户端 IP：%s", body)
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

func TestAPIChallengeRejectsRequestThatReachesThreshold(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	tracker := newAbuseTracker(cfg)
	now := time.Now().UTC()
	for i := 0; i < cfg.Challenge.Exact.RejectBurst-1; i++ {
		tracker.record("api", "192.0.2.9/32", 1, now)
	}
	srv := Server{AbuseTracker: tracker}
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

func TestFullPublicSourceKeepsExactIPAndNetwork(t *testing.T) {
	tests := map[string]string{
		"192.0.2.9/32":       "192.0.2.9",
		"192.0.2.0/24":       "192.0.2.0/24",
		"2001:db8::9/128":    "2001:db8::9",
		"2001:db8:abcd::/48": "2001:db8:abcd::/48",
	}
	for input, want := range tests {
		if got := fullPublicSource(input); got != want {
			t.Fatalf("fullPublicSource(%q)=%q，期望 %q", input, got, want)
		}
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

func TestManualBlockRequestImmediatelyIncrementsVisibleAttempts(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	insertTestClientBlock(t, db, "192.0.2.9/32", "manual", now)
	cfg := testAbuseControl("enforce", true)
	var console bytes.Buffer
	logger, err := logging.New("master", config.Logging{
		ConsoleLevel: "debug", FileLevel: "debug", Directory: t.TempDir(), RetentionDays: 1,
	}, time.Local, &console)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	srv := Server{
		Store: Store{DB: db}, Blocklist: newBlocklistPolicy(config.Quota{}, nil),
		ClientBlocks: newClientBlockManager(db, cfg, nil), AbuseTracker: newAbuseTracker(cfg),
		Logger: logger,
	}
	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	if !srv.rejectBlockedDownload(rec, req, "asset-1", "download_page") {
		t.Fatal("人工封禁请求应被拒绝")
	}
	srv.ClientBlocks.mu.Lock()
	pending := srv.ClientBlocks.pending["192.0.2.9/32"]
	srv.ClientBlocks.mu.Unlock()
	if pending == nil || pending.delta != 1 {
		t.Fatalf("人工封禁待写次数=%+v，期望 delta=1", pending)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rec.Body.String(), "192.0.2.9") {
		t.Fatalf("普通封禁页应为 HTML 并显示完整 IP：type=%q body=%s",
			rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if line := console.String(); !strings.Contains(line, "客户端来源=192.0.2.9") ||
		!strings.Contains(line, "触发封禁次数=1") {
		t.Fatalf("封禁日志应显示完整 IP 和自增次数：%s", line)
	}
}

func TestExistingManualBlockAboveThresholdTriggersOnNextRequest(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	insertTestClientBlock(t, db, "192.0.2.9/32", "manual", now)
	if _, err := db.Exec(`UPDATE client_blocks SET attempts_after_block = 303
		WHERE client_prefix_key = '192.0.2.9/32'`); err != nil {
		t.Fatal(err)
	}
	cfg := testAbuseControl("enforce", true)
	srv := Server{
		Store: Store{DB: db}, Blocklist: newBlocklistPolicy(config.Quota{}, nil),
		ClientBlocks: newClientBlockManager(db, cfg, nil), AbuseTracker: newAbuseTracker(cfg),
	}
	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	if !srv.rejectBlockedDownload(rec, req, "asset-1", "download_page") {
		t.Fatal("人工封禁请求应被拒绝")
	}
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "punishment-pow.js") {
		t.Fatalf("已超过阈值的人工封禁应在下一次请求进入惩罚页：code=%d body=%s",
			rec.Code, rec.Body.String())
	}
	var punishment int
	var level int
	if err := db.QueryRow(`SELECT punishment_active, escalation_level FROM client_blocks
		WHERE client_prefix_key = '192.0.2.9/32'`).Scan(&punishment, &level); err != nil {
		t.Fatal(err)
	}
	if punishment != 1 || level != 0 {
		t.Fatalf("人工封禁应只激活惩罚、不升级期限：punishment=%d level=%d", punishment, level)
	}
}
