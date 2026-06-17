package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/protocol"
)

func acknowledgeInventoryReconcileResult(ctx context.Context, tx *sql.Tx, nodeID string,
	result protocol.SyncTaskResult, now string) (string, error) {
	_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET
		error_message = ?, updated_at = ?
		WHERE id = ? AND node_id = ? AND task_type = 'inventory_reconcile'
		AND state IN ('sent', 'running')`,
		nullable(result.Message), now, result.TaskID, nodeID)
	return "awaiting_inventory", err
}

func completeInventoryReconcileTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'succeeded',
		error_message = NULL, completed_at = ?, retry_after = NULL,
		lease_expires_at = NULL, updated_at = ?
		WHERE node_id = ? AND task_type = 'inventory_reconcile'
		AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		now, now, nodeID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
