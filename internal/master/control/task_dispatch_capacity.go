package control

import (
	"context"
	"database/sql"
)

func (r Repository) outstandingSentSyncTasks(ctx context.Context, nodeID string) (int, error) {
	var count int
	err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state = 'sent'`, nodeID).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}
