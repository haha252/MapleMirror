package public

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"
)

func (m *clientBlockManager) resolve(ctx context.Context, clientPrefix string, now time.Time) (clientBlockDecision, error) {
	if m == nil || m.store == nil || m.store.DB == nil || strings.TrimSpace(clientPrefix) == "" || clientPrefix == "unknown" {
		return clientBlockDecision{}, nil
	}
	keys := clientBlockLookupKeys(clientPrefix)
	if len(keys) == 0 {
		return clientBlockDecision{}, nil
	}
	m.mu.Lock()
	// Prefer cached positive decisions first.
	for _, key := range keys {
		if cached, ok := m.cache[key]; ok {
			if !cached.record.isBlocked(now) {
				delete(m.cache, key)
				continue
			}
			if now.Before(cached.revalidateAt) {
				decision := m.decisionFromRecordLocked(key, cached.record)
				m.mu.Unlock()
				return decision, nil
			}
		}
	}
	// A miss is conclusive only when every lookup scope has a fresh negative
	// entry. For IPv4, a host miss alone says nothing about its /24 (and vice
	// versa), especially after an exact-key management invalidation.
	allNegative := true
	for _, key := range keys {
		cached, ok := m.cache[key]
		if !ok || cached.record.Blocked || !now.Before(cached.negativeAt) {
			allNegative = false
			break
		}
	}
	if allNegative {
		m.mu.Unlock()
		return clientBlockDecision{}, nil
	}
	m.mu.Unlock()

	record, key, ok, err := m.store.lookupClientBlock(ctx, clientPrefix, now)
	if err != nil {
		if decision, cached := m.cachedPositive(keys, now); cached {
			if m.logger != nil {
				m.logger.Warn(ctx, "客户端封禁缓存查询失败，继续使用正缓存",
					slog.String("client_source", fullPublicSource(clientPrefix)),
					slog.String("error", err.Error()))
			}
			return decision, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			m.mu.Lock()
			m.cacheNegativeLocked(keys, now)
			m.mu.Unlock()
		} else if m.logger != nil {
			m.logger.Warn(ctx, "客户端封禁查询失败，临时放行",
				slog.String("client_source", fullPublicSource(clientPrefix)),
				slog.String("error", err.Error()))
		}
		return clientBlockDecision{}, nil
	}
	if !ok {
		m.mu.Lock()
		m.cacheNegativeLocked(keys, now)
		m.mu.Unlock()
		return clientBlockDecision{}, nil
	}
	decision := clientBlockDecision{
		Blocked:         true,
		Reason:          record.Reason,
		Source:          record.Source,
		Key:             key,
		Attempts:        record.AttemptsAfterBlock,
		EscalationLevel: record.EscalationLevel,
		BlockedAt:       record.BlockedAt,
		ExpiresAt:       record.ExpiresAt,
	}
	decision.PunishmentActive = m.punishmentActive(record, key)
	m.mu.Lock()
	m.cache[key] = clientBlockCacheEntry{
		record:       record,
		fetchedAt:    now,
		revalidateAt: now.Add(m.revalidateEvery),
	}
	m.mu.Unlock()
	return decision, nil
}

func (m *clientBlockManager) cacheNegativeLocked(keys []string, now time.Time) {
	for _, key := range keys {
		m.ensureCacheCapacityLocked()
		m.cache[key] = clientBlockCacheEntry{negativeAt: now.Add(m.negativeTTL)}
	}
}

func (m *clientBlockManager) ensureCacheCapacityLocked() {
	if len(m.cache) < clientBlockCacheCapacity {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, entry := range m.cache {
		at := entry.fetchedAt
		if at.IsZero() {
			at = entry.negativeAt
		}
		if oldestKey == "" || at.Before(oldest) {
			oldestKey, oldest = key, at
		}
	}
	if oldestKey != "" {
		delete(m.cache, oldestKey)
	}
}

func (m *clientBlockManager) cachedPositive(keys []string, now time.Time) (clientBlockDecision, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		if cached, ok := m.cache[key]; ok && cached.record.Blocked && cached.record.isBlocked(now) {
			return m.decisionFromRecordLocked(key, cached.record), true
		}
	}
	return clientBlockDecision{}, false
}

func (m *clientBlockManager) decisionFromRecordLocked(key string, record clientBlockRecord) clientBlockDecision {
	return clientBlockDecision{
		Blocked:          true,
		Reason:           record.Reason,
		Source:           record.Source,
		Key:              key,
		Attempts:         record.AttemptsAfterBlock,
		EscalationLevel:  record.EscalationLevel,
		PunishmentActive: m.punishmentActive(record, key),
		BlockedAt:        record.BlockedAt,
		ExpiresAt:        record.ExpiresAt,
	}
}
