package public

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func testAbuseControl(mode string, punishment bool) config.AbuseControl {
	return config.AbuseControl{
		Mode: mode,
		Challenge: config.AbuseChallenge{
			BurstWindow: "1m", RollingWindow: "10m", InvalidSolutionWeight: 4,
			ElevatedBits: 2, SevereBits: 4, MaxBits: 28,
			Exact: config.AbuseThresholdWindow{
				ElevatedBurst: 6, ElevatedRolling: 15, SevereBurst: 12,
				SevereRolling: 25, RejectBurst: 20, RejectRolling: 30,
			},
			Network: config.AbuseThresholdWindow{
				ElevatedBurst: 60, ElevatedRolling: 150, SevereBurst: 120,
				SevereRolling: 250, RejectBurst: 200, RejectRolling: 300,
			},
		},
		Blocked: config.AbuseBlocked{
			FlushInterval: "1h", FlushBatch: 100, CacheNegative: "30s",
			Escalation: config.AbuseEscalation{
				Level1Attempts: 10, Level1Duration: "720h",
				Level2Attempts: 50, Level2Duration: "2160h",
				Level3Attempts: 200, Level3Duration: "8760h",
			},
		},
		Punishment: config.AbusePunishment{
			Enabled: &punishment, Difficulty: 128, TotalAttempts: 30,
			BurstAttempts: 20, RollingAttempts: 30, WorkerLimit: 32,
		},
	}
}

func insertTestClientBlock(t *testing.T, db *sql.DB, key, source string, now time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, escalation_level, punishment_active, last_attempt_at, updated_at)
		VALUES (?, 'test', ?, ?, ?, 0, 0, 0, ?, ?)`, key, source,
		now.Format(time.RFC3339Nano), now.Add(7*24*time.Hour).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientBlockManagerConcurrentFlushKeepsEveryAttempt(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	insertTestClientBlock(t, db, "192.0.2.9/32", "local_auto_ban", now)
	m := newClientBlockManager(db, testAbuseControl("enforce", true), nil)
	m.flushBatch = 7
	decision := clientBlockDecision{Blocked: true, Source: "local_auto_ban", Key: "192.0.2.9/32"}

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.recordAttempt(context.Background(), decision, time.Now().UTC())
		}()
		if i%31 == 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m.flushPending(context.Background(), time.Now().UTC())
			}()
		}
	}
	wg.Wait()
	m.flushPending(context.Background(), time.Now().UTC())

	var attempts int64
	if err := db.QueryRow(`SELECT attempts_after_block FROM client_blocks
		WHERE client_prefix_key = '192.0.2.9/32'`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1000 {
		t.Fatalf("并发封禁计数=%d，期望 1000", attempts)
	}
}

func TestApplyBlockedAttemptsKeepsPunishmentIndependentFromExpiry(t *testing.T) {
	db := openMaster(t)
	store := Store{DB: db}
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	insertTestClientBlock(t, db, "192.0.2.9/32", "local_auto_ban", now)
	m := newClientBlockManager(db, testAbuseControl("enforce", true), nil)
	policy := m.applyPolicy()

	result, err := store.applyBlockedAttempts(context.Background(), "192.0.2.9/32", 10,
		now, 10, 10, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.EscalationLevel != 1 || result.Record.PunishmentActive {
		t.Fatalf("10 次状态错误：%+v", result.Record)
	}
	level1Expiry := result.Record.ExpiresAt
	result, err = store.applyBlockedAttempts(context.Background(), "192.0.2.9/32", 20,
		now.Add(time.Minute), 20, 30, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.EscalationLevel != 1 || !result.Record.PunishmentActive || result.Record.ExpiresAt != level1Expiry {
		t.Fatalf("30 次只应激活惩罚，不应改变期限：%+v", result.Record)
	}
	result, err = store.applyBlockedAttempts(context.Background(), "192.0.2.9/32", 20,
		now.Add(2*time.Minute), 1, 1, policy)
	if err != nil || result.Record.EscalationLevel != 2 {
		t.Fatalf("50 次应进入二级封禁：record=%+v err=%v", result.Record, err)
	}
	result, err = store.applyBlockedAttempts(context.Background(), "192.0.2.9/32", 150,
		now.Add(3*time.Minute), 1, 1, policy)
	if err != nil || result.Record.EscalationLevel != 3 {
		t.Fatalf("200 次应进入三级封禁：record=%+v err=%v", result.Record, err)
	}
}

func TestApplyBlockedAttemptsHonorsModesAndScope(t *testing.T) {
	tests := []struct {
		name, mode, key, source string
		enabled                 bool
		wantLevel               int
		wantPunishment          bool
	}{
		{name: "off", mode: "off", key: "192.0.2.9/32", source: "local_auto_ban", enabled: true},
		{name: "observe", mode: "observe", key: "192.0.2.9/32", source: "local_auto_ban", enabled: true},
		{name: "disabled", mode: "enforce", key: "192.0.2.9/32", source: "local_auto_ban", enabled: false, wantLevel: 1},
		{name: "manual", mode: "enforce", key: "192.0.2.9/32", source: "manual", enabled: true, wantPunishment: true},
		{name: "network", mode: "enforce", key: "192.0.2.0/24", source: "local_auto_ban", enabled: true},
		{name: "eligible", mode: "enforce", key: "192.0.2.9/32", source: "local_auto_ban", enabled: true, wantLevel: 1, wantPunishment: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openMaster(t)
			now := time.Now().UTC()
			insertTestClientBlock(t, db, tt.key, tt.source, now)
			m := newClientBlockManager(db, testAbuseControl(tt.mode, tt.enabled), nil)
			result, err := m.store.applyBlockedAttempts(context.Background(), tt.key, 30,
				now, 30, 30, m.applyPolicy())
			if err != nil {
				t.Fatal(err)
			}
			if result.Record.EscalationLevel != tt.wantLevel || result.Record.PunishmentActive != tt.wantPunishment {
				t.Fatalf("状态错误：%+v", result.Record)
			}
		})
	}
}

func TestFailedImmediateFlushRequeuesDelta(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	insertTestClientBlock(t, db, "192.0.2.9/32", "local_auto_ban", now)
	m := newClientBlockManager(db, testAbuseControl("enforce", true), nil)
	m.mu.Lock()
	m.pending["192.0.2.9/32"] = &pendingBlockFlush{delta: 1, lastAt: now}
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.flushKey(ctx, "192.0.2.9/32", now); err == nil {
		t.Fatal("已取消上下文应导致刷新失败")
	}
	if err := m.flushKey(context.Background(), "192.0.2.9/32", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var attempts int64
	if err := db.QueryRow(`SELECT attempts_after_block FROM client_blocks
		WHERE client_prefix_key = '192.0.2.9/32'`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("重试后次数=%d，期望 1", attempts)
	}
}

func TestExpiredAutoBlockStartsWithCleanAbuseState(t *testing.T) {
	db := openMaster(t)
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	_, err := db.Exec(`INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, escalation_level, punishment_active, last_attempt_at, updated_at)
		VALUES ('192.0.2.9/32', 'old', 'local_auto_ban', ?, ?, 200, 3, 1, ?, ?)`,
		old.Format(time.RFC3339Nano), old.Add(time.Hour).Format(time.RFC3339Nano),
		old.Format(time.RFC3339Nano), old.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{DB: db}
	if err := store.upsertAutoBlock(context.Background(), "192.0.2.9/32", "new",
		"local_auto_ban", now, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	var attempts int64
	var level, punishment int
	if err := db.QueryRow(`SELECT attempts_after_block, escalation_level, punishment_active
		FROM client_blocks WHERE client_prefix_key = '192.0.2.9/32'`).
		Scan(&attempts, &level, &punishment); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || level != 0 || punishment != 0 {
		t.Fatalf("过期封禁状态未重置：attempts=%d level=%d punishment=%d", attempts, level, punishment)
	}
}

func TestBlockedAttemptWindowPruneDoesNotRefreshLastUsed(t *testing.T) {
	now := time.Now().UTC()
	lastUsed := now.Add(-25 * time.Hour)
	window := &blockedAttemptWindow{
		events:   []time.Time{now.Add(-11 * time.Minute)},
		lastUsed: lastUsed,
	}
	window.prune(now)
	if !window.lastUsed.Equal(lastUsed) {
		t.Fatalf("prune 修改了 lastUsed：got=%s want=%s", window.lastUsed, lastUsed)
	}
	if len(window.events) != 0 {
		t.Fatalf("过期窗口事件未清理：%d", len(window.events))
	}
}

func TestPunishmentStateHiddenWhileDisabled(t *testing.T) {
	m := newClientBlockManager(nil, testAbuseControl("enforce", false), nil)
	record := clientBlockRecord{Source: "local_auto_ban", PunishmentActive: true}
	if m.punishmentActive(record, "192.0.2.9/32") {
		t.Fatal("关闭惩罚后不应向请求路径展示已有惩罚状态")
	}
}
