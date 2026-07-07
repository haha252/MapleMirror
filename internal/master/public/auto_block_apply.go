package public

import (
	"context"
	"database/sql"
	"time"
)

type clientBlockApplyPolicy struct {
	Enforce           bool
	PunishmentEnabled bool
	EscalationLevel1  int64
	EscalationLevel2  int64
	EscalationLevel3  int64
	EscalationDur1    time.Duration
	EscalationDur2    time.Duration
	EscalationDur3    time.Duration
	PunishmentTotal   int64
	PunishmentBurst   int64
	PunishmentRolling int64
}

type clientBlockApplyResult struct {
	Record             clientBlockRecord
	PreviousEscalation int
	PreviousPunishment bool
}

func (s Store) applyBlockedAttempts(ctx context.Context, key string, delta int64,
	now time.Time, burst, rolling int64, policy clientBlockApplyPolicy) (clientBlockApplyResult, error) {
	if s.DB == nil {
		return clientBlockApplyResult{}, sql.ErrConnDone
	}
	if delta <= 0 {
		return clientBlockApplyResult{}, nil
	}
	nowText := now.Format(time.RFC3339Nano)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return clientBlockApplyResult{}, err
	}
	defer tx.Rollback()

	record, punishment, err := incrementBlockedAttempts(ctx, tx, key, delta, nowText)
	if err != nil {
		return clientBlockApplyResult{}, err
	}
	result := clientBlockApplyResult{
		PreviousEscalation: record.EscalationLevel,
		PreviousPunishment: record.PunishmentActive,
	}
	applyBlockPolicy(&record, key, burst, rolling, now, policy)
	punishment = 0
	if record.PunishmentActive {
		punishment = 1
	}
	record.LastAttemptAt = nowText
	update, err := tx.ExecContext(ctx, `UPDATE client_blocks SET
		escalation_level = ?, punishment_active = ?,
		expires_at = ?, last_attempt_at = ?, updated_at = ?
		WHERE client_prefix_key = ? AND expires_at > ?`,
		record.EscalationLevel, punishment, record.ExpiresAt,
		nowText, nowText, key, nowText)
	if err != nil {
		return clientBlockApplyResult{}, err
	}
	if affected, err := update.RowsAffected(); err != nil {
		return clientBlockApplyResult{}, err
	} else if affected == 0 {
		return clientBlockApplyResult{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return clientBlockApplyResult{}, err
	}
	result.Record = record
	return result, nil
}

func incrementBlockedAttempts(ctx context.Context, tx *sql.Tx, key string, delta int64,
	nowText string) (clientBlockRecord, int, error) {
	var record clientBlockRecord
	var punishment int
	err := tx.QueryRowContext(ctx, `UPDATE client_blocks SET
		attempts_after_block = attempts_after_block + ?, last_attempt_at = ?, updated_at = ?
		WHERE client_prefix_key = ? AND expires_at > ?
		RETURNING client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, escalation_level, punishment_active, last_attempt_at`,
		delta, nowText, nowText, key, nowText).
		Scan(&record.ClientPrefixKey, &record.Reason, &record.Source, &record.BlockedAt,
			&record.ExpiresAt, &record.AttemptsAfterBlock, &record.EscalationLevel,
			&punishment, &record.LastAttemptAt)
	if err != nil {
		return clientBlockRecord{}, 0, err
	}
	record.Blocked = true
	record.PunishmentActive = punishment == 1
	return record, punishment, nil
}

func applyBlockPolicy(record *clientBlockRecord, key string, burst, rolling int64,
	now time.Time, policy clientBlockApplyPolicy) {
	eligible := policy.Enforce && record.Source == "local_auto_ban" &&
		validPunishmentClientBlockPrefix(key)
	if !eligible {
		return
	}
	targetLevel := applyEscalationLevel(record.AttemptsAfterBlock, policy)
	if targetLevel > record.EscalationLevel {
		record.EscalationLevel = targetLevel
		record.ExpiresAt = extendedBlockExpiry(record.ExpiresAt, now,
			applyEscalationDuration(targetLevel, policy))
	}
	if policy.PunishmentEnabled && (record.AttemptsAfterBlock >= policy.PunishmentTotal ||
		burst >= policy.PunishmentBurst || rolling >= policy.PunishmentRolling) {
		record.PunishmentActive = true
	}
}

func applyEscalationLevel(total int64, policy clientBlockApplyPolicy) int {
	switch {
	case total >= policy.EscalationLevel3:
		return 3
	case total >= policy.EscalationLevel2:
		return 2
	case total >= policy.EscalationLevel1:
		return 1
	default:
		return 0
	}
}

func applyEscalationDuration(level int, policy clientBlockApplyPolicy) time.Duration {
	switch level {
	case 1:
		return policy.EscalationDur1
	case 2:
		return policy.EscalationDur2
	case 3:
		return policy.EscalationDur3
	default:
		return 0
	}
}

func extendedBlockExpiry(current string, now time.Time, duration time.Duration) string {
	if duration <= 0 {
		return current
	}
	candidate := now.Add(duration)
	parsed, err := time.Parse(time.RFC3339Nano, current)
	if err != nil || candidate.After(parsed) {
		return candidate.Format(time.RFC3339Nano)
	}
	return current
}
