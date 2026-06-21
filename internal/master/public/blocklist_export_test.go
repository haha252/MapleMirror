package public

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestBlocklistTXTExportsMergedActiveBlocks(t *testing.T) {
	db := openMaster(t)
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	mustExec(t, db, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES ('192.0.2.9/32', 'traffic_limit_exceeded', 'local_auto_ban',
		'2026-06-21T11:00:00Z', '2026-06-22T11:00:00Z', 3,
		'2026-06-21T11:30:00Z', '2026-06-21T11:30:00Z')`)
	mustExec(t, db, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES ('198.51.100.0/24', '人工预封禁', 'manual',
		'2026-06-21T11:00:00Z', '2026-06-22T11:00:00Z', 1,
		'2026-06-21T11:30:00Z', '2026-06-21T11:30:00Z')`)
	mustExec(t, db, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES ('203.0.113.10/32', 'expired', 'local_auto_ban',
		'2026-06-19T11:00:00Z', '2026-06-20T11:00:00Z', 1,
		'2026-06-19T11:30:00Z', '2026-06-19T11:30:00Z')`)
	policy := newBlocklistPolicy(config.Quota{
		Blocklist: config.Blocklist{Static: []string{"192.0.2.9"}},
	}, nil)
	policy.feedItems["https://feed.example.test/all.txt"] = []blocklistEntry{
		{prefix: mustBlockPrefix(t, "2001:db8::/32"), source: "2001:db8::/32", note: "[Sparkle3 不受信任投票] 封禁计数: 8"},
	}
	srv := Server{Store: Store{DB: db}, Blocklist: policy}

	entries, err := srv.blocklistExportEntries(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	body := string(renderBlocklistTXT(entries))
	for _, want := range []string{
		"# [枫源镜像封禁] 封禁原因: static_blocklist; traffic_limit_exceeded, 来源: 192.0.2.9; local_auto_ban, 封禁后尝试次数: 3",
		"192.0.2.9\n",
		"# [枫源镜像封禁] 封禁原因: 人工预封禁, 来源: manual, 封禁后尝试次数: 1",
		"198.51.100.0/24\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected blocklist txt to contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, "2001:db8::/32") || strings.Contains(body, "Sparkle3") {
		t.Fatalf("remote feed blocks should not be exported: %s", body)
	}
	if strings.Contains(body, "203.0.113.10") {
		t.Fatalf("expired block should not be exported: %s", body)
	}
	if strings.Count(body, "192.0.2.9\n") != 1 {
		t.Fatalf("duplicate blocks should be merged: %s", body)
	}
}

func TestBlocklistTXTHandler(t *testing.T) {
	srv := Server{Blocklist: newBlocklistPolicy(config.Quota{
		Blocklist: config.Blocklist{Static: []string{"192.0.2.9"}},
	}, nil)}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/v1/blocklist.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type=%q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# [枫源镜像封禁] 封禁原因: static_blocklist") ||
		!strings.Contains(body, "\n192.0.2.9\n") {
		t.Fatalf("unexpected blocklist body: %s", body)
	}
}

func TestBlocklistTXTCacheKeepsBodyUntilTTL(t *testing.T) {
	db := openMaster(t)
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	cache := newBlocklistExportCache(time.Minute)
	srv := Server{Store: Store{DB: db}, BlocklistExport: cache}
	first, err := srv.cachedBlocklistTXT(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES ('192.0.2.9/32', 'traffic_limit_exceeded', 'local_auto_ban',
		'2026-06-21T12:00:00Z', '2026-06-22T12:00:00Z', 0,
		'2026-06-21T12:00:00Z', '2026-06-21T12:00:00Z')`)

	second, err := srv.cachedBlocklistTXT(context.Background(), now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) || strings.Contains(string(second), "192.0.2.9") {
		t.Fatalf("TTL 内应返回缓存内容，first=%q second=%q", first, second)
	}
	third, err := srv.cachedBlocklistTXT(context.Background(), now.Add(61*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(third), "192.0.2.9") {
		t.Fatalf("TTL 后应刷新封禁列表：%s", third)
	}
}

func mustBlockPrefix(t *testing.T, raw string) netip.Prefix {
	t.Helper()
	prefix, err := parseBlockPrefix(raw)
	if err != nil {
		t.Fatal(err)
	}
	return prefix
}
