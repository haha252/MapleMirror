package control

import (
	"context"
	"database/sql"
	"time"
)

func (r Repository) outstandingSentSyncTasks(ctx context.Context, nodeID string) (int, error) {
	var count int
	err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state = 'sent'
		AND lease_expires_at IS NOT NULL AND lease_expires_at != ''
		AND lease_expires_at > ?`,
		nodeID, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}
