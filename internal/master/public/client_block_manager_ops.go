package public

import (
	"context"
	"time"
)

func (m *clientBlockManager) recordAttempt(ctx context.Context, decision clientBlockDecision, now time.Time) {
	if m == nil || !decision.Blocked || decision.Key == "" {
		return
	}
	m.mu.Lock()
	win := m.windows[decision.Key]
	if win == nil {
		win = &blockedAttemptWindow{}
		m.windows[decision.Key] = win
	}
	win.add(now)
	pending := m.pending[decision.Key]
	if pending == nil {
		pending = &pendingBlockFlush{}
		m.pending[decision.Key] = pending
	}
	pending.delta++
	pending.lastAt = now
	total := decision.Attempts + pending.delta
	burst := win.countWindow(now, time.Minute)
	rolling := win.countWindow(now, 10*time.Minute)
	needPersist := pending.delta >= m.flushBatch || m.shouldPersistState(decision, total, burst, rolling)
	m.mu.Unlock()
	if needPersist {
		_ = m.flushKey(ctx, decision.Key, now)
	}
}

func (m *clientBlockManager) shouldPersistState(decision clientBlockDecision, total, burst, rolling int64) bool {
	if !decision.Blocked || !m.enforcing() || !validPunishmentClientBlockPrefix(decision.Key) {
		return false
	}
	if punishmentEligibleStoredSource(decision.Source) && m.punishmentEnabled && !decision.PunishmentActive &&
		(total >= m.punishmentTotal || burst >= m.punishmentBurst || rolling >= m.punishmentRolling) {
		return true
	}
	if decision.Source != "local_auto_ban" {
		return false
	}
	target := m.escalationLevel(total)
	return target > decision.EscalationLevel
}

func (m *clientBlockManager) escalationLevel(total int64) int {
	switch {
	case total >= m.escalationLevel3:
		return 3
	case total >= m.escalationLevel2:
		return 2
	case total >= m.escalationLevel1:
		return 1
	default:
		return 0
	}
}

func (m *clientBlockManager) claimPendingSnapshotLocked(key string, now time.Time) (entry *pendingBlockFlush, burst, rolling int64) {
	pending := m.pending[key]
	if pending == nil {
		m.pending[key] = &pendingBlockFlush{lastAt: now}
	} else {
		entry = pending
		m.pending[key] = &pendingBlockFlush{lastAt: now}
	}
	if win := m.windows[key]; win != nil {
		burst = win.countWindow(now, time.Minute)
		rolling = win.countWindow(now, 10*time.Minute)
	}
	return entry, burst, rolling
}
