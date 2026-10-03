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
	1 * time.Minute,
	3 * time.Minute,
}

func (r Repository) applyTaskResult(ctx context.Context, tx *sql.Tx, nodeID string, result protocol.SyncTaskResult, now time.Time) (string, int, string, error) {
	var attempts int
	var state, leaseExpires, taskType string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(attempts, 0), state, task_type,
		COALESCE(lease_expires_at, '') FROM node_tasks
		WHERE id = ? AND node_id = ?`, result.TaskID, nodeID).
		Scan(&attempts, &state, &taskType, &leaseExpires); err != nil {
		return "", 0, "", err
	}

	nowText := now.Format(time.RFC3339Nano)
	if !syncTaskCompletable(state, leaseExpires, nowText) {
		return "stale_result", attempts, "", nil
	}
	current, err := syncTaskCurrent(ctx, tx, nodeID, result.TaskID, taskType, result.AssetID)
	if err != nil {
		return "", 0, "", err
	}
	if !current {
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'obsolete',
			error_message = '任务资产已不在当前目标库存中', completed_at = ?,
			updated_at = ?, retry_after = NULL, lease_expires_at = NULL
			WHERE id = ? AND node_id = ?`,
			nowText, nowText, result.TaskID, nodeID)
		return "obsolete", attempts, "", err
	}
	if taskType == "inventory_reconcile" && result.Result == "succeeded" {
		state, err := acknowledgeInventoryReconcileResult(ctx, tx, nodeID, result, nowText)
		return state, attempts, "", err
	}
	switch result.Result {
	case "succeeded":
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'succeeded',
			error_message = ?, completed_at = ?, updated_at = ?, retry_after = NULL,
			lease_expires_at = NULL
			WHERE id = ? AND node_id = ?`,
			nullable(result.Message), nowText, nowText, result.TaskID, nodeID)
		return "succeeded", attempts, "", err
	case "temporary_error", "digest_mismatch", "size_mismatch":
		nextAttempts := attempts + 1
		if result.Result == "temporary_error" && !result.PeerFallbackAttempted &&
			r.hasUsablePeerFallback(ctx, tx, nodeID, protocol.SyncTask{
				TaskID:   result.TaskID,
				TaskType: taskType,
				Asset:    protocol.SyncAsset{AssetID: result.AssetID},
			}) {
			_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
				attempts = ?, error_message = ?, retry_after = NULL,
				lease_expires_at = NULL, updated_at = ?
				WHERE id = ? AND node_id = ?`,
				nextAttempts, nullable(result.Message), nowText, result.TaskID, nodeID)
			return "pending", nextAttempts, "", err
		}
		keepRetrying := result.Result == "temporary_error" && r.retryableV2SwarmTask(ctx, tx, nodeID, result.AssetID)
		if nextAttempts > len(retryBackoffSchedule) && !keepRetrying {
			_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'failed',
				attempts = ?, error_message = ?, retry_after = NULL,
				lease_expires_at = NULL, updated_at = ?
				WHERE id = ? AND node_id = ?`,
				nextAttempts, nullable(result.Message), nowText, result.TaskID, nodeID)
			return "failed", nextAttempts, "", err
		}
		backoffIndex := nextAttempts - 1
		if backoffIndex >= len(retryBackoffSchedule) {
			backoffIndex = len(retryBackoffSchedule) - 1
		}
		retryAfter := now.Add(retryBackoffSchedule[backoffIndex]).Format(time.RFC3339Nano)
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'retry_wait',
			attempts = ?, error_message = ?, retry_after = ?,
			lease_expires_at = NULL, updated_at = ?
			WHERE id = ? AND node_id = ?`,
			nextAttempts, nullable(result.Message), retryAfter, nowText, result.TaskID, nodeID)
		return "retry_wait", nextAttempts, retryAfter, err
	default:
		_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'failed',
			error_message = ?, retry_after = NULL, lease_expires_at = NULL,
			updated_at = ?
			WHERE id = ? AND node_id = ?`,
			nullable(result.Message), nowText, result.TaskID, nodeID)
		return "failed", attempts, "", err
	}
}

func syncTaskCompletable(state, leaseExpires, now string) bool {
	return (state == "sent" || state == "running") && leaseExpires != "" && leaseExpires >= now
}

func syncTaskCurrent(ctx context.Context, tx *sql.Tx, nodeID, taskID, taskType, assetID string) (bool, error) {
	if assetID == "" {
		return true, nil
	}
	if taskType == "asset_delete" {
		var ok int
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM node_tasks t
			JOIN target_inventory ti ON ti.node_id = t.node_id
				AND ti.asset_id = t.asset_id AND ti.desired_state = 'remove'
			WHERE t.id = ? AND t.node_id = ? AND t.asset_id = ?
		)`, taskID, nodeID, assetID).Scan(&ok)
		return ok == 1, err
	}
	var ok int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM node_tasks t
		JOIN target_inventory ti ON ti.node_id = t.node_id
			AND ti.asset_id = t.asset_id AND ti.desired_state = 'required'
		JOIN assets a ON a.id = t.asset_id
			AND a.service_state IN ('candidate', 'pending', 'active')
		JOIN releases r ON r.id = a.release_id AND r.selected = 1
		JOIN projects p ON p.id = r.project_id AND p.enabled = 1
		WHERE t.id = ? AND t.node_id = ? AND t.asset_id = ?
	)`, taskID, nodeID, assetID).Scan(&ok)
	return ok == 1, err
}
