package control

import (
	"context"
	"database/sql"
)

func updateDownloadHistoryTraffic(ctx context.Context, tx *sql.Tx,
	authorizationID string, sentBytes int64, now string) error {
	if sentBytes <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE download_history SET
		sent_bytes = sent_bytes + ?,
		first_transfer_at = CASE WHEN first_transfer_at = '' THEN ? ELSE first_transfer_at END,
		last_transfer_at = ?,
		status = CASE WHEN status = 'issued' THEN 'active' ELSE status END,
		updated_at = ?
		WHERE authorization_id = ?`,
		sentBytes, now, now, now, authorizationID)
	return err
}

func updateDownloadHistoryStatus(ctx context.Context, tx *sql.Tx,
	authorizationID, status, reason, occurredAt string) error {
	_, err := tx.ExecContext(ctx, `UPDATE download_history
		SET status = ?, status_reason = ?, updated_at = ?
		WHERE authorization_id = ?`,
		status, reason, occurredAt, authorizationID)
	return err
}
