package public

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"
)

func (m *clientBlockManager) flushPending(ctx context.Context, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if len(m.pending) == 0 {
		m.mu.Unlock()
		return
	}
	batch := m.pending
	m.pending = map[string]*pendingBlockFlush{}
	m.mu.Unlock()
	for key, entry := range batch {
		m.applyPendingEntry(ctx, key, entry, now)
	}
}

func (m *clientBlockManager) flushKey(ctx context.Context, key string, now time.Time) error {
	if m == nil || key == "" {
		return nil
	}
	lock := m.flushLock(key)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	entry, burst, rolling := m.claimPendingSnapshotLocked(key, now)
	m.mu.Unlock()
	if entry == nil || entry.delta <= 0 {
		return nil
	}
	if !entry.nextRetry.IsZero() && now.Before(entry.nextRetry) {
		m.mergePending(key, entry)
		return nil
	}
	return m.applyClaimedEntry(ctx, key, entry, now, burst, rolling)
}

func (m *clientBlockManager) applyPendingEntry(ctx context.Context, key string,
	entry *pendingBlockFlush, now time.Time) {
	if entry == nil || entry.delta <= 0 {
		return
	}
	if !entry.nextRetry.IsZero() && now.Before(entry.nextRetry) {
		m.mergePending(key, entry)
		return
	}
	lock := m.flushLock(key)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	var burst, rolling int64
	if win := m.windows[key]; win != nil {
		burst = win.countWindow(now, time.Minute)
		rolling = win.countWindow(now, 10*time.Minute)
	}
	m.mu.Unlock()
	_ = m.applyClaimedEntry(ctx, key, entry, now, burst, rolling)
}

func (m *clientBlockManager) applyClaimedEntry(ctx context.Context, key string,
	entry *pendingBlockFlush, now time.Time, burst, rolling int64) error {
	result, err := m.store.applyBlockedAttempts(ctx, key, entry.delta, now, burst, rolling, m.applyPolicy())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.invalidate(key)
			return nil
		}
		entry.failCount++
		entry.nextRetry = now.Add(backoffDuration(entry.failCount))
		m.mergePending(key, entry)
		if m.logger != nil && shouldLogAttempt(int64(entry.failCount)) {
			m.logger.Warn(ctx, "封禁后计数刷新失败",
				slog.String("client_source", fullPublicSource(key)),
				slog.Int("consecutive_failures", entry.failCount),
				slog.String("error", err.Error()))
		}
		return err
	}
	m.cacheAppliedResult(key, result.Record, now)
	m.logStateTransition(ctx, key, result)
	return nil
}

func (m *clientBlockManager) applyPolicy() clientBlockApplyPolicy {
	return clientBlockApplyPolicy{
		Enforce: m.enforcing(), PunishmentEnabled: m.punishmentEnabled,
		EscalationLevel1: m.escalationLevel1, EscalationLevel2: m.escalationLevel2,
		EscalationLevel3: m.escalationLevel3, EscalationDur1: m.escalationDur1,
		EscalationDur2: m.escalationDur2, EscalationDur3: m.escalationDur3,
		PunishmentTotal: m.punishmentTotal, PunishmentBurst: m.punishmentBurst,
		PunishmentRolling: m.punishmentRolling,
	}
}

func (m *clientBlockManager) cacheAppliedResult(key string, record clientBlockRecord, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureCacheCapacityLocked()
	m.cache[key] = clientBlockCacheEntry{
		record: record, fetchedAt: now, revalidateAt: now.Add(m.revalidateEvery),
	}
}

func (m *clientBlockManager) logStateTransition(ctx context.Context, key string, result clientBlockApplyResult) {
	if m.logger == nil {
		return
	}
	if result.Record.EscalationLevel > result.PreviousEscalation {
		m.logger.Warn(ctx, "自动封禁期限已升级",
			slog.String("client_source", fullPublicSource(key)),
			slog.Int64("blocked_after_attempts", result.Record.AttemptsAfterBlock),
			slog.Int("escalation_level", result.Record.EscalationLevel),
			slog.String("expires_at", result.Record.ExpiresAt))
	}
	if result.Record.PunishmentActive && !result.PreviousPunishment {
		m.logger.Warn(ctx, "客户端已进入惩罚验证模式",
			slog.String("client_source", fullPublicSource(key)),
			slog.Int64("blocked_after_attempts", result.Record.AttemptsAfterBlock))
	}
}

func (m *clientBlockManager) mergePending(key string, entry *pendingBlockFlush) {
	if entry == nil || entry.delta <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := m.pending[key]
	if pending == nil {
		pending = &pendingBlockFlush{}
		m.pending[key] = pending
	}
	pending.delta += entry.delta
	if pending.nextRetry.IsZero() || (!entry.nextRetry.IsZero() && entry.nextRetry.After(pending.nextRetry)) {
		pending.nextRetry = entry.nextRetry
	}
	if entry.failCount > pending.failCount {
		pending.failCount = entry.failCount
	}
}

func (m *clientBlockManager) cleanup(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, entry := range m.cache {
		if entry.record.Blocked {
			if now.After(entry.revalidateAt.Add(24 * time.Hour)) {
				delete(m.cache, key)
			}
			continue
		}
		if now.After(entry.negativeAt.Add(24 * time.Hour)) {
			delete(m.cache, key)
		}
	}
	for key, pending := range m.pending {
		if pending == nil || (pending.delta == 0 && now.Sub(pending.lastAt) > 24*time.Hour) {
			delete(m.pending, key)
		}
	}
	for key, win := range m.windows {
		if win == nil || now.Sub(win.lastUsed) > 24*time.Hour {
			delete(m.windows, key)
			continue
		}
		win.prune(now)
		if len(win.events) == 0 && now.Sub(win.lastUsed) > time.Hour {
			delete(m.windows, key)
		}
	}
}

func (m *clientBlockManager) invalidateAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = map[string]clientBlockCacheEntry{}
	m.pending = map[string]*pendingBlockFlush{}
	m.windows = map[string]*blockedAttemptWindow{}
}
