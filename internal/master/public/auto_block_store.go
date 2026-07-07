package public

import (
	"context"
	"database/sql"
	"net/netip"
	"time"
)

type clientBlockRecord struct {
	ClientPrefixKey    string
	Reason             string
	Source             string
	Blocked            bool
	BlockedAt          string
	ExpiresAt          string
	AttemptsAfterBlock int64
	EscalationLevel    int
	PunishmentActive   bool
	LastAttemptAt      string
}

func (s Store) ActiveAutoBlock(ctx context.Context, clientPrefix string, now time.Time) (blockDecision, error) {
	nowText := now.Format(time.RFC3339Nano)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return blockDecision{}, err
	}
	defer tx.Rollback()
	for _, key := range clientBlockLookupKeys(clientPrefix) {
		var reason, source string
		var attempts int64
		err = tx.QueryRowContext(ctx, `UPDATE client_blocks SET
			attempts_after_block = attempts_after_block + 1,
			last_attempt_at = ?, updated_at = ?
			WHERE client_prefix_key = ? AND expires_at > ?
			RETURNING reason, source, attempts_after_block`,
			nowText, nowText, key, nowText).Scan(&reason, &source, &attempts)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return blockDecision{}, err
		}
		if err := tx.Commit(); err != nil {
			return blockDecision{}, err
		}
		return blockDecision{
			Blocked: true, Reason: reason, Source: source, Attempts: attempts,
		}, nil
	}
	return blockDecision{}, nil
}

func (s Store) lookupClientBlock(ctx context.Context, clientPrefix string, now time.Time) (clientBlockRecord, string, bool, error) {
	if s.DB == nil {
		return clientBlockRecord{}, "", false, sql.ErrNoRows
	}
	nowText := now.Format(time.RFC3339Nano)
	for _, key := range clientBlockLookupKeys(clientPrefix) {
		var record clientBlockRecord
		var punishment int
		err := s.DB.QueryRowContext(ctx, `SELECT client_prefix_key, reason, source, blocked_at, expires_at,
			attempts_after_block, escalation_level, punishment_active, last_attempt_at
			FROM client_blocks WHERE client_prefix_key = ? AND expires_at > ?`,
			key, nowText).Scan(&record.ClientPrefixKey, &record.Reason, &record.Source,
			&record.BlockedAt, &record.ExpiresAt, &record.AttemptsAfterBlock,
			&record.EscalationLevel, &punishment, &record.LastAttemptAt)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return clientBlockRecord{}, "", false, err
		}
		record.PunishmentActive = punishment == 1
		record.Blocked = true
		return record, key, true, nil
	}
	return clientBlockRecord{}, "", false, sql.ErrNoRows
}

func (s Store) upsertAutoBlock(ctx context.Context, clientPrefix, reason, source string,
	now time.Time, ttl time.Duration) error {
	if s.DB == nil {
		return sql.ErrConnDone
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(ttl).Format(time.RFC3339Nano)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, escalation_level, punishment_active,
		last_attempt_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, 0, 0, ?, ?)
		ON CONFLICT(client_prefix_key) DO UPDATE SET
		reason = excluded.reason,
		source = excluded.source,
		blocked_at = CASE
			WHEN client_blocks.expires_at <= excluded.blocked_at THEN excluded.blocked_at
			ELSE client_blocks.blocked_at
		END,
		expires_at = CASE
			WHEN client_blocks.expires_at > excluded.expires_at THEN client_blocks.expires_at
			ELSE excluded.expires_at
		END,
		attempts_after_block = CASE
			WHEN client_blocks.expires_at <= excluded.blocked_at THEN 0
			ELSE client_blocks.attempts_after_block
		END,
		escalation_level = CASE
			WHEN client_blocks.expires_at <= excluded.blocked_at THEN 0
			ELSE client_blocks.escalation_level
		END,
		punishment_active = CASE
			WHEN client_blocks.expires_at <= excluded.blocked_at THEN 0
			ELSE client_blocks.punishment_active
		END,
		last_attempt_at = CASE
			WHEN client_blocks.expires_at <= excluded.blocked_at THEN excluded.last_attempt_at
			ELSE client_blocks.last_attempt_at
		END,
		updated_at = excluded.updated_at`,
		clientPrefix, reason, source, nowText, expires, nowText, nowText)
	return err
}

func (s Store) upsertManualClientBlock(ctx context.Context, clientPrefix, reason string,
	now time.Time, ttl time.Duration) error {
	if s.DB == nil {
		return sql.ErrConnDone
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(ttl).Format(time.RFC3339Nano)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, escalation_level, punishment_active,
		last_attempt_at, updated_at)
		VALUES (?, ?, 'manual', ?, ?, 0, 0, 0, ?, ?)
		ON CONFLICT(client_prefix_key) DO UPDATE SET
		reason = excluded.reason,
		source = excluded.source,
		blocked_at = excluded.blocked_at,
		expires_at = excluded.expires_at,
		attempts_after_block = 0,
		escalation_level = 0,
		punishment_active = 0,
		last_attempt_at = excluded.last_attempt_at,
		updated_at = excluded.updated_at`,
		clientPrefix, reason, nowText, expires, nowText, nowText)
	return err
}

func (s Store) clearClientBlock(ctx context.Context, key string) error {
	if s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM client_blocks WHERE client_prefix_key = ?`, key)
	return err
}

func clientBlockLookupKeys(clientPrefix string) []string {
	keys := []string{clientPrefix}
	prefix, err := netip.ParsePrefix(clientPrefix)
	if err != nil {
		return keys
	}
	addr := prefix.Addr()
	if !addr.Is4() {
		return keys
	}
	parent := netip.PrefixFrom(addr, 24).Masked().String()
	if parent != clientPrefix {
		keys = append(keys, parent)
	}
	return keys
}

func (s Store) AutoBlockClient(ctx context.Context, clientPrefix, reason, source string,
	now time.Time, ttl time.Duration) (blockDecision, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(ttl).Format(time.RFC3339Nano)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(client_prefix_key) DO UPDATE SET
		reason = excluded.reason,
		source = excluded.source,
		expires_at = excluded.expires_at,
		updated_at = excluded.updated_at`,
		clientPrefix, reason, source, nowText, expires, nowText, nowText)
	if err != nil {
		return blockDecision{}, err
	}
	return blockDecision{Blocked: true, Reason: reason, Source: source}, nil
}
