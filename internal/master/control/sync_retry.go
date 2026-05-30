package control

import (
	"context"
	"database/sql"
	"time"

	"mirror-server/internal/protocol"
)

var retryBackoffSchedule = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

func (r Repository) applyTaskResult(ctx context.Context, tx *sql.Tx, nodeID string, result protocol.SyncTaskResult, now time.Time) (string, int, string, error) {
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(attempts, 0) FROM node_tasks
		WHERE id = ? AND node_id = ?`, result.TaskID, nodeID).Scan(&attempts); err != nil {
		return "", 0, "", err
	}

	nowText := now.Format(time.RFC3339Nano)
	switch result.Result {
	case "succeeded":
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'succeeded',
			error_message = ?, completed_at = ?, updated_at = ?, retry_after = NULL
			WHERE id = ? AND node_id = ?`,
			nullable(result.Message), nowText, nowText, result.TaskID, nodeID)
		return "succeeded", attempts, "", err
	case "temporary_error":
		nextAttempts := attempts + 1
		if nextAttempts > len(retryBackoffSchedule) {
			_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'failed',
				attempts = ?, error_message = ?, retry_after = NULL, updated_at = ?
				WHERE id = ? AND node_id = ?`,
				nextAttempts, nullable(result.Message), nowText, result.TaskID, nodeID)
			return "failed", nextAttempts, "", err
		}
		retryAfter := now.Add(retryBackoffSchedule[nextAttempts-1]).Format(time.RFC3339Nano)
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'retry_wait',
			attempts = ?, error_message = ?, retry_after = ?, updated_at = ?
			WHERE id = ? AND node_id = ?`,
			nextAttempts, nullable(result.Message), retryAfter, nowText, result.TaskID, nodeID)
		return "retry_wait", nextAttempts, retryAfter, err
	default:
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'failed',
			error_message = ?, retry_after = NULL, updated_at = ?
			WHERE id = ? AND node_id = ?`,
			nullable(result.Message), nowText, result.TaskID, nodeID)
		return "failed", attempts, "", err
	}
}
