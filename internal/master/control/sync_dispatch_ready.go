package control

import (
	"context"
	"time"
)

func (r Repository) hasDispatchableSyncTask(ctx context.Context, nodeID string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var ok int
	err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM node_tasks t
		WHERE t.node_id = ? AND (
			t.state = 'pending'
			OR (t.state = 'retry_wait' AND (t.retry_after IS NULL OR t.retry_after = '' OR t.retry_after <= ?))
			OR (t.state IN ('sent', 'running') AND (
				t.lease_expires_at IS NULL OR t.lease_expires_at = '' OR t.lease_expires_at <= ?
			))
		)`+eligibleSyncTaskSQL("t")+`
	)`, nodeID, now, now).Scan(&ok)
	return ok == 1, err
}
