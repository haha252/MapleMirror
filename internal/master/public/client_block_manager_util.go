package public

import (
	"hash/fnv"
	"net/netip"
	"strings"
	"sync"
	"time"
)

func (w *blockedAttemptWindow) add(now time.Time) {
	if w == nil {
		return
	}
	w.events = append(w.events, now)
	w.lastUsed = now
	w.prune(now)
}

func (w *blockedAttemptWindow) count(now time.Time) int64 {
	return w.countWindow(now, 10*time.Minute)
}

func (w *blockedAttemptWindow) countWindow(now time.Time, window time.Duration) int64 {
	if w == nil {
		return 0
	}
	w.prune(now)
	cutoff := now.Add(-window)
	var count int64
	for _, ts := range w.events {
		if ts.After(cutoff) || ts.Equal(cutoff) {
			count++
		}
	}
	return count
}

func (w *blockedAttemptWindow) prune(now time.Time) {
	if w == nil {
		return
	}
	cutoff := now.Add(-10 * time.Minute)
	idx := 0
	for idx < len(w.events) && w.events[idx].Before(cutoff) {
		idx++
	}
	if idx > 0 {
		w.events = append([]time.Time(nil), w.events[idx:]...)
	}
}

func backoffDuration(failCount int) time.Duration {
	if failCount <= 0 {
		return time.Second
	}
	delay := time.Second << min(failCount-1, 8)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (r clientBlockRecord) isBlocked(now time.Time) bool {
	if r.ExpiresAt == "" {
		return false
	}
	expires, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	return err == nil && now.Before(expires)
}

func validClientBlockPrefix(key string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(key))
	if err != nil {
		return false
	}
	if prefix.Addr().Is4() {
		return prefix.Bits() == 24 || prefix.Bits() == 32
	}
	return prefix.Bits() == 128
}

func validPunishmentClientBlockPrefix(key string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(key))
	if err != nil {
		return false
	}
	if prefix.Addr().Is4() {
		return prefix.Bits() == 32
	}
	return prefix.Bits() == 128
}

func isIPv4NetworkBlock(key string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(key))
	return err == nil && prefix.Addr().Is4() && prefix.Bits() == 24
}

func (m *clientBlockManager) flushLock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &m.flushLocks[h.Sum32()%uint32(len(m.flushLocks))]
}

func (m *clientBlockManager) enforcing() bool {
	return m != nil && m.mode == "enforce"
}

func (m *clientBlockManager) punishmentActive(record clientBlockRecord, key string) bool {
	if m == nil || !m.enforcing() || !m.punishmentEnabled ||
		record.Source != "local_auto_ban" || !validPunishmentClientBlockPrefix(key) {
		return false
	}
	return record.PunishmentActive
}

func (m *clientBlockManager) estimatedAttempts(key string, persisted int64) int64 {
	if m == nil {
		return persisted
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if pending := m.pending[key]; pending != nil {
		return persisted + pending.delta
	}
	return persisted
}

func shouldLogAttempt(attempts int64) bool {
	return attempts <= 1 || (attempts > 0 && attempts&(attempts-1) == 0)
}
