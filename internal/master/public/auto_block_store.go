package public

import (
	"context"
	"database/sql"
	"net/netip"
	"time"
)

func (s Store) ActiveAutoBlock(ctx context.Context, clientPrefix string, now time.Time) (blockDecision, error) {
	var reason, source string
	var attempts int64
	var blockKey string
	nowText := now.Format(time.RFC3339Nano)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return blockDecision{}, err
	}
	defer tx.Rollback()
	for _, key := range clientBlockLookupKeys(clientPrefix) {
		err = tx.QueryRowContext(ctx, `SELECT reason, source, attempts_after_block
			FROM client_blocks WHERE client_prefix_key = ? AND expires_at > ?`,
			key, nowText).Scan(&reason, &source, &attempts)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return blockDecision{}, err
		}
		blockKey = key
		break
	}
	if blockKey == "" {
		return blockDecision{}, nil
	}
	attempts++
	_, err = tx.ExecContext(ctx, `UPDATE client_blocks SET attempts_after_block = ?,
		last_attempt_at = ?, updated_at = ? WHERE client_prefix_key = ?`,
		attempts, nowText, nowText, blockKey)
	if err != nil {
		return blockDecision{}, err
	}
	return blockDecision{
		Blocked: true, Reason: reason, Source: source, Attempts: attempts,
	}, tx.Commit()
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
