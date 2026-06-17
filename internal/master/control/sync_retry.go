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
		if result.Result == "temporary_error" && r.hasVerifiedPeer(ctx, tx, nodeID, result.AssetID) &&
			attempts == 0 {
			_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
				attempts = ?, error_message = ?, retry_after = NULL,
				lease_expires_at = NULL, updated_at = ?
				WHERE id = ? AND node_id = ?`,
				nextAttempts, nullable(result.Message), nowText, result.TaskID, nodeID)
			return "pending", nextAttempts, "", err
		}
		if nextAttempts > len(retryBackoffSchedule) {
			_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'failed',
				attempts = ?, error_message = ?, retry_after = NULL,
				lease_expires_at = NULL, updated_at = ?
				WHERE id = ? AND node_id = ?`,
				nextAttempts, nullable(result.Message), nowText, result.TaskID, nodeID)
			return "failed", nextAttempts, "", err
		}
		retryAfter := now.Add(retryBackoffSchedule[nextAttempts-1]).Format(time.RFC3339Nano)
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
			AND a.service_state IN ('candidate', 'pending')
		JOIN releases r ON r.id = a.release_id AND r.selected = 1
		JOIN projects p ON p.id = r.project_id AND p.enabled = 1
		WHERE t.id = ? AND t.node_id = ? AND t.asset_id = ?
	)`, taskID, nodeID, assetID).Scan(&ok)
	return ok == 1, err
}

func (r Repository) hasVerifiedPeer(ctx context.Context, tx *sql.Tx, nodeID, assetID string) bool {
	if assetID == "" {
		return false
	}
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
		JOIN assets a ON a.id = ni.asset_id
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = ni.asset_id AND ti.desired_state = 'required'
		WHERE ni.asset_id = ? AND ni.node_id != ? AND n.state NOT IN ('disabled', 'offline')
		AND n.last_heartbeat_at IS NOT NULL AND n.last_heartbeat_at != ''
		AND n.public_download_base_url != '' AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
		AND a.service_state IN ('candidate', 'pending') AND r.selected = 1 AND p.enabled = 1
		AND `+r.syncPeerPublicProbeSQL()+`
	)`, r.syncPeerArgs(assetID, nodeID)...).Scan(&exists)
	return err == nil && exists == 1
}
