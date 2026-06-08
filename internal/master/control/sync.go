package control

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) NextSyncTask(ctx context.Context, nodeID string) (protocol.SyncTask, bool, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	defer tx.Rollback()
	task, ok, err := r.claimNextSyncTask(ctx, tx, nodeID)
	if err != nil || !ok {
		return protocol.SyncTask{}, ok, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.SyncTask{}, false, err
	}
	if task.TaskType == "asset_download" {
		task.FallbackSources = r.syncFallbackSources(ctx, nodeID, task)
	}
	return task, true, nil
}

func (r Repository) claimNextSyncTask(ctx context.Context, tx *sql.Tx, nodeID string) (protocol.SyncTask, bool, error) {
	var task protocol.SyncTask
	var attempts int
	var retryAfter string
	var assetID, projectID, version, fileName, downloadURL, digest sql.NullString
	var size sql.NullInt64
	nowValue := time.Now().UTC()
	now := nowValue.Format(time.RFC3339Nano)
	leaseExpires := nowValue.Add(syncTaskLeaseDuration).Format(time.RFC3339Nano)
	err := tx.QueryRowContext(ctx, `SELECT t.id, t.task_type, a.id, r.project_id,
		r.tag_name, a.file_name, a.size_bytes, a.source_url, a.digest_sha256, COALESCE(t.attempts, 0),
		COALESCE(t.retry_after, '')
		FROM node_tasks t LEFT JOIN assets a ON a.id = t.asset_id
		LEFT JOIN releases r ON r.id = a.release_id
		WHERE t.node_id = ?
		AND (
			t.state = 'pending'
			OR (t.state = 'retry_wait' AND (t.retry_after IS NULL OR t.retry_after = '' OR t.retry_after <= ?))
		)`+eligibleSyncTaskSQL("t")+`
	ORDER BY r.published_at DESC, a.size_bytes, t.created_at LIMIT 1`, nodeID, now).
		Scan(&task.TaskID, &task.TaskType, &assetID,
			&projectID, &version, &fileName, &size,
			&downloadURL, &digest,
			&attempts, &retryAfter)
	if err == sql.ErrNoRows {
		return protocol.SyncTask{}, false, nil
	}
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	task.Asset = protocol.SyncAsset{
		AssetID: assetID.String, ProjectID: projectID.String, Version: version.String,
		FileName: fileName.String, SizeBytes: size.Int64,
		DownloadURL: downloadURL.String, DigestSHA256: digest.String,
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'sent',
		lease_expires_at = ?, updated_at = ? WHERE id = ? AND node_id = ? AND (
		state = 'pending'
			OR (state = 'retry_wait' AND (retry_after IS NULL OR retry_after = '' OR retry_after <= ?))
		)`+eligibleSyncTaskSQL("node_tasks")+``, leaseExpires, now, task.TaskID, nodeID, now)
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return protocol.SyncTask{}, false, nil
	}
	if err == nil && r.Logger != nil {
		r.Logger.Debug(ctx, "同步任务可派发",
			slog.String("node_id", nodeID),
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("asset_id", task.Asset.AssetID),
			slog.Int("attempts", attempts),
			slog.String("retry_after", retryAfter))
	}
	return task, true, nil
}

func (r Repository) AcceptSyncTaskResult(ctx context.Context, session Session, seq uint64, result protocol.SyncTaskResult) (HeartbeatResult, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	nowValue := time.Now().UTC()
	now := nowValue.Format(time.RFC3339Nano)
	checkedResult, inventory, hasInventory, err := inspectSucceededSyncResult(ctx, tx, result)
	if err != nil {
		return HeartbeatResult{}, err
	}
	taskState, attempts, retryAfter, err := r.applyTaskResult(ctx, tx, session.NodeID, checkedResult, nowValue)
	if err != nil {
		return HeartbeatResult{}, err
	}
	quarantined := false
	if hasInventory && taskState != "obsolete" {
		if err := upsertSyncResultInventory(ctx, tx, session.NodeID, inventory, now); err != nil {
			return HeartbeatResult{}, err
		}
		if inventory.PublicAsset && inventory.State == "mismatch" {
			if err := r.quarantineNodeForPublicAssetMismatch(ctx, tx, session, inventory, now); err != nil {
				return HeartbeatResult{}, err
			}
			quarantined = true
		}
		if inventory.State == "verified" {
			if err := publishVerifiedAsset(ctx, tx, inventory.AssetID, now); err != nil {
				return HeartbeatResult{}, err
			}
		}
	}
	ready, err := r.reconcileNodeReady(ctx, tx, session.NodeID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	err = r.updateSequence(session, seq)
	if err == nil && r.Logger != nil {
		r.Logger.Debug(ctx, "同步任务结果已处理",
			slog.String("node_id", session.NodeID),
			slog.String("task_id", checkedResult.TaskID),
			slog.String("asset_id", checkedResult.AssetID),
			slog.String("result", checkedResult.Result),
			slog.String("task_state", taskState),
			slog.Int("attempts", attempts),
			slog.String("retry_after", retryAfter),
			slog.String("message", checkedResult.Message))
	}
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	if quarantined {
		r.runtime().CloseNodeSessions(session.NodeID)
		return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(false), RoutingReady: false}, nil
	}
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, nil
}

func (r Repository) reconcileNodeReady(ctx context.Context, tx *sql.Tx, nodeID, now string) (bool, error) {
	var previousReady int
	var nodeState string
	var lastHeartbeat string
	if err := tx.QueryRowContext(ctx, `SELECT routing_ready, state,
		COALESCE(last_heartbeat_at, '') FROM nodes WHERE id = ?`, nodeID).
		Scan(&previousReady, &nodeState, &lastHeartbeat); err != nil {
		return false, err
	}
	var missing, running int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID).Scan(&missing); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		nodeID).Scan(&running); err != nil {
		return false, err
	}
	ready := 0
	if nodeState != "disabled" && nodeState != "offline" && lastHeartbeat != "" &&
		missing == 0 && running == 0 {
		ready = 1
	}
	_, err := tx.ExecContext(ctx, `UPDATE nodes SET routing_ready = ?,
		updated_at = ? WHERE id = ?`, ready, now, nodeID)
	if err == nil && r.Logger != nil && previousReady != ready {
		r.Logger.Debug(ctx, "节点同步就绪状态已更新",
			slog.String("node_id", nodeID),
			slog.Bool("routing_ready", ready == 1),
			slog.Int("missing_targets", missing),
			slog.Int("running_tasks", running),
			slog.String("connection_state", nodeState))
	}
	return ready == 1, err
}

func readySnapshot(ctx context.Context, tx *sql.Tx, nodeID string) (missing, running int, ready bool) {
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID).Scan(&missing)
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		nodeID).Scan(&running)
	var readyInt int
	_ = tx.QueryRowContext(ctx, `SELECT routing_ready FROM nodes WHERE id = ?`, nodeID).Scan(&readyInt)
	return missing, running, readyInt == 1
}

func (r Repository) currentSequence(session Session) (uint64, error) {
	last, err := r.runtime().CurrentSequence(session)
	if err != nil {
		return 0, fmt.Errorf("%w", ErrSessionUnavailable)
	}
	return last, nil
}

func (r Repository) updateSequence(session Session, seq uint64) error {
	if err := r.runtime().UpdateSequence(session, seq); err != nil {
		return fmt.Errorf("%w", ErrSessionUnavailable)
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
