package developerapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) Consume(ctx context.Context, projectID, token string, now time.Time) (Usage, error) {
	usage := s.usageAt(now)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return usage, err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT token_hash FROM project_developer_tokens
		WHERE project_id = ?`, projectID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored != tokenHash(token)) {
		return usage, ErrUnauthorized
	}
	if err != nil {
		return usage, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO project_developer_api_daily_usage
		(project_id, usage_date, request_count, updated_at) VALUES (?, ?, 1, ?)
		ON CONFLICT(project_id, usage_date) DO UPDATE SET
		request_count = request_count + 1, updated_at = excluded.updated_at
		WHERE request_count < ?`,
		projectID, s.dayKey(now), now.UTC().Format(time.RFC3339Nano), usage.Limit)
	if err != nil {
		return usage, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		current, err := s.usageCountTx(ctx, tx, projectID, now)
		if err != nil {
			return usage, err
		}
		usage.Used = current
		usage.Remaining = max(0, usage.Limit-current)
		return usage, ErrRateLimited
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_developer_tokens
		SET last_used_at = ? WHERE project_id = ?`,
		now.UTC().Format(time.RFC3339Nano), projectID); err != nil {
		return usage, err
	}
	if err := tx.Commit(); err != nil {
		return usage, err
	}
	usage.Used, err = s.usageCount(ctx, projectID, now)
	usage.Remaining = max(0, usage.Limit-usage.Used)
	return usage, err
}

func (s *Store) Usage(ctx context.Context, projectID string, now time.Time) (Usage, error) {
	usage := s.usageAt(now)
	count, err := s.usageCount(ctx, projectID, now)
	usage.Used = count
	usage.Remaining = max(0, usage.Limit-count)
	return usage, err
}

func (s *Store) RecentRunningScan(ctx context.Context, projectID string, now time.Time) (bool, error) {
	var exists int
	cutoff := now.UTC().Add(-30 * time.Minute).Format(time.RFC3339Nano)
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM project_scan_state WHERE project_id = ?
		AND last_scan_state = 'running' AND last_scan_started_at >= ?)`,
		projectID, cutoff).Scan(&exists)
	return exists == 1, err
}

func (s *Store) usageCount(ctx context.Context, projectID string, now time.Time) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT request_count
		FROM project_developer_api_daily_usage
		WHERE project_id = ? AND usage_date = ?`, projectID, s.dayKey(now)).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

func (s *Store) usageCountTx(ctx context.Context, tx *sql.Tx,
	projectID string, now time.Time) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT request_count
		FROM project_developer_api_daily_usage
		WHERE project_id = ? AND usage_date = ?`, projectID, s.dayKey(now)).Scan(&count)
	return count, err
}

func (s *Store) usageAt(now time.Time) Usage {
	local := now.In(s.Location)
	y, m, d := local.Date()
	reset := time.Date(y, m, d+1, 0, 0, 0, 0, s.Location)
	return Usage{Limit: s.limit(), Remaining: s.limit(), ResetAt: reset}
}

func (s *Store) dayKey(now time.Time) string {
	return now.In(s.Location).Format("2006-01-02")
}
