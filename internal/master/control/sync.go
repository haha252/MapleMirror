package control

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) NextSyncTask(ctx context.Context, nodeID string) (protocol.SyncTask, bool, error) {
	var task protocol.SyncTask
	err := r.DB.QueryRowContext(ctx, `SELECT t.id, t.task_type, a.id, a.file_name,
		a.size_bytes, a.source_url, a.digest_sha256
		FROM node_tasks t LEFT JOIN assets a ON a.id = t.asset_id
		WHERE t.node_id = ? AND t.state IN ('pending', 'retry_wait')
		ORDER BY t.created_at LIMIT 1`, nodeID).
		Scan(&task.TaskID, &task.TaskType, &task.Asset.AssetID,
			&task.Asset.FileName, &task.Asset.SizeBytes,
			&task.Asset.DownloadURL, &task.Asset.DigestSHA256)
	if err == sql.ErrNoRows {
		return protocol.SyncTask{}, false, nil
	}
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	_, err = r.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'sent',
		updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), task.TaskID)
	return task, true, err
}

func (r Repository) AcceptSyncTaskResult(ctx context.Context, session Session, seq uint64, result protocol.SyncTaskResult) (HeartbeatResult, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	last, err := currentSequence(ctx, tx, session.ID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		return HeartbeatResult{AcceptedSequence: last, ManagedState: "syncing"}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	state := taskState(result.Result)
	_, err = tx.ExecContext(ctx, `UPDATE node_tasks SET state = ?,
		error_message = ?, completed_at = CASE WHEN ? = 'succeeded' THEN ? ELSE completed_at END,
		updated_at = ? WHERE id = ? AND node_id = ?`,
		state, nullable(result.Message), state, now, now, result.TaskID, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if result.Result == "succeeded" {
		if err := upsertVerifiedInventory(ctx, tx, session.NodeID, result, now); err != nil {
			return HeartbeatResult{}, err
		}
	}
	if err := reconcileNodeReady(ctx, tx, session.NodeID, now); err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET
		last_message_sequence = ? WHERE id = ?`, seq, session.ID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, finish(tx, err)
}

func upsertVerifiedInventory(ctx context.Context, tx *sql.Tx, nodeID string, result protocol.SyncTaskResult, now string) error {
	var expectedDigest string
	var expectedSize int64
	err := tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, result.AssetID).Scan(&expectedDigest, &expectedSize)
	if err != nil {
		return err
	}
	state := "verified"
	if result.LocalDigestSHA256 != expectedDigest || result.SizeBytes != expectedSize {
		state = "mismatch"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
		state = excluded.state`,
		nodeID, result.AssetID, result.LocalDigestSHA256, result.SizeBytes, now, state)
	return err
}

func reconcileNodeReady(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	var missing, running int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID).Scan(&missing); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		nodeID).Scan(&running); err != nil {
		return err
	}
	ready := 0
	state := "syncing"
	if missing == 0 && running == 0 {
		ready = 1
		state = "ready"
	}
	_, err := tx.ExecContext(ctx, `UPDATE nodes SET routing_ready = ?,
		state = CASE WHEN state = 'disabled' THEN state ELSE ? END,
		updated_at = ? WHERE id = ?`, ready, state, now, nodeID)
	return err
}

func currentSequence(ctx context.Context, tx *sql.Tx, sessionID string) (uint64, error) {
	var last uint64
	err := tx.QueryRowContext(ctx, `SELECT last_message_sequence FROM node_control_sessions
		WHERE id = ? AND disconnected_at IS NULL`, sessionID).Scan(&last)
	if err != nil {
		return 0, fmt.Errorf("控制会话不可用")
	}
	return last, nil
}

func taskState(result string) string {
	if result == "succeeded" {
		return "succeeded"
	}
	if result == "temporary_error" {
		return "retry_wait"
	}
	return "failed"
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
