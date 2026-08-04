package public

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
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
		if !strings.Contains(rec.Body.String(), `"source":"192.0.2.9"`) ||
			strings.Contains(rec.Body.String(), "被AI封禁") {
			t.Fatalf("封禁提示应包含完整客户端 IP：%s", rec.Body.String())
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
	entries, err := parseBlocklistFeed(strings.NewReader("192.0.2.1\n192.0.2.1/32\n# comment\n2001:db8::/32\nbad\n"), "feed")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("订阅源应只保留有效 IP/CIDR：%+v", entries)
	}
	if entries[1].note != "comment" {
		t.Fatalf("订阅源应保留 IP 前一行注释：%+v", entries)
	}
}

type failingBlocklistReader struct {
	data []byte
	done bool
}

func (r *failingBlocklistReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, errors.New("模拟订阅源读取失败")
	}
	r.done = true
	return copy(p, r.data), nil
}

func TestParseBlocklistFeedReturnsReadError(t *testing.T) {
	entries, err := parseBlocklistFeed(&failingBlocklistReader{
		data: []byte("192.0.2.1\n"),
	}, "feed")
	if err == nil {
		t.Fatal("订阅源读取失败时应返回错误")
	}
	if entries != nil {
		t.Fatalf("订阅源读取失败时不应返回部分结果：%+v", entries)
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

func TestManualIPv4SegmentBlockMatchesClientHost(t *testing.T) {
	db := openMaster(t)
	store := Store{DB: db}
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES (?, ?, 'manual', ?, ?, 0, ?, ?)`,
		"192.0.2.0/24", "人工预封禁", now.Format(time.RFC3339Nano),
		now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	decision, err := store.ActiveAutoBlock(context.Background(), "192.0.2.9/32", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Blocked || decision.Attempts != 1 || decision.Source != "manual" {
		t.Fatalf("manual /24 block should match host prefix: %+v", decision)
	}
	var attempts int
	if err := db.QueryRow(`SELECT attempts_after_block FROM client_blocks WHERE client_prefix_key = '192.0.2.0/24'`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("manual /24 attempts = %d, want 1", attempts)
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

func TestRejectBlockedDownloadLogsCanceledLookupAsDebug(t *testing.T) {
	db := openMaster(t)
	var console bytes.Buffer
	logger, err := logging.New("master", config.Logging{
		ConsoleLevel: "debug", FileLevel: "debug", Directory: t.TempDir(), RetentionDays: 1,
	}, time.Local, &console)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges", nil).WithContext(ctx)
	req.RemoteAddr = "192.0.2.9:12345"

	server := Server{
		Store:     Store{DB: db},
		Blocklist: newBlocklistPolicy(config.Quota{}, nil),
		Logger:    logger,
	}
	rec := httptest.NewRecorder()
	if server.rejectBlockedDownload(rec, req, "asset-1", "api_challenge") {
		t.Fatal("已取消请求不应被误判为已封禁")
	}

	line := console.String()
	if !strings.Contains(line, "调试 master 请求已取消，自动封禁状态查询终止") {
		t.Fatalf("expected debug log for canceled request, got: %s", line)
	}
	if strings.Contains(line, "警告 master 自动封禁状态查询失败，继续处理请求") {
		t.Fatalf("canceled request should not log warn: %s", line)
	}
}

func TestRejectBlockedDownloadKeepsWarnForOtherLookupErrors(t *testing.T) {
	db := openMaster(t)
	_ = db.Close()
	var console bytes.Buffer
	logger, err := logging.New("master", config.Logging{
		ConsoleLevel: "debug", FileLevel: "debug", Directory: t.TempDir(), RetentionDays: 1,
	}, time.Local, &console)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges", nil)
	req.RemoteAddr = "192.0.2.9:12345"

	server := Server{
		Store:     Store{DB: db},
		Blocklist: newBlocklistPolicy(config.Quota{}, nil),
		Logger:    logger,
	}
	rec := httptest.NewRecorder()
	if server.rejectBlockedDownload(rec, req, "asset-1", "api_challenge") {
		t.Fatal("数据库故障不应被误判为已封禁")
	}

	line := console.String()
	if !strings.Contains(line, "警告 master 自动封禁状态查询失败，继续处理请求") {
		t.Fatalf("expected warn log for non-context error, got: %s", line)
	}
}
