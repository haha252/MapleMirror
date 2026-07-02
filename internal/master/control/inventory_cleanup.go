package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/assignment"
)

// queueNodeInventoryCleanup makes stale files eligible for deletion without
// waiting for the next complete inventory report. The master's accepted
// inventory already identifies verified replicas outside the required set.
func queueNodeInventoryCleanup(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	if _, err := markNonRequiredVerifiedInventoryRemoved(ctx, tx, nodeID, now); err != nil {
		return 0, err
	}
	return assignment.GenerateNodeDeleteTasks(ctx, tx, nodeID, now)
}

func markNonRequiredVerifiedInventoryRemoved(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT ni.node_id, ni.asset_id, 'remove', ?
		FROM node_inventory ni
		JOIN assets a ON a.id = ni.asset_id
		LEFT JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = ni.asset_id AND ti.desired_state = 'required'
		WHERE ni.node_id = ? AND ni.state = 'verified' AND ti.asset_id IS NULL
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		desired_state = excluded.desired_state,
		updated_at = excluded.updated_at`,
		now, nodeID)
	if err != nil {
		return 0, err
	}
	changed, _ := result.RowsAffected()
	return int(changed), nil
}
