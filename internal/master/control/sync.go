package control

import (
	"context"
	"database/sql"
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

func (r Repository) refreshExpiredSyncTaskLeases(ctx context.Context, nodeID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, lease_expires_at = NULL, updated_at = ?
		WHERE node_id = ? AND state IN ('sent', 'running')
		AND lease_expires_at IS NOT NULL AND lease_expires_at != ''
		AND lease_expires_at <= ?`,
		now, nodeID, now)
	return err
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
	err := tx.QueryRowContext(ctx, `WITH candidate AS (
		SELECT t.id FROM node_tasks t
		LEFT JOIN assets a ON a.id = t.asset_id
		LEFT JOIN releases r ON r.id = a.release_id
		WHERE t.node_id = ?
		AND (
			t.state = 'pending'
			OR (t.state = 'retry_wait' AND (t.retry_after IS NULL OR t.retry_after = '' OR t.retry_after <= ?))
			OR (t.state IN ('sent', 'running') AND (
				t.lease_expires_at IS NULL OR t.lease_expires_at = '' OR t.lease_expires_at <= ?
			))
		)`+eligibleSyncTaskSQL("t")+`
		ORDER BY r.published_at DESC, a.size_bytes, t.created_at LIMIT 1
	)
	UPDATE node_tasks SET state = 'sent', lease_expires_at = ?, updated_at = ?
	WHERE id = (SELECT id FROM candidate) AND node_id = ?
	RETURNING id, task_type, asset_id, COALESCE(attempts, 0),
		COALESCE(retry_after, '')`, nodeID, now, now, leaseExpires, now, nodeID).
		Scan(&task.TaskID, &task.TaskType, &assetID, &attempts, &retryAfter)
	if err == sql.ErrNoRows {
		return protocol.SyncTask{}, false, nil
	}
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT a.id, r.project_id,
		r.tag_name, a.file_name, a.size_bytes, a.source_url, a.digest_sha256
		FROM node_tasks t LEFT JOIN assets a ON a.id = t.asset_id
		LEFT JOIN releases r ON r.id = a.release_id
		WHERE t.id = ? AND t.node_id = ?`,
		task.TaskID, nodeID).
		Scan(&assetID, &projectID, &version, &fileName, &size,
			&downloadURL, &digest)
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	task.Asset = protocol.SyncAsset{
		AssetID: assetID.String, ProjectID: projectID.String, Version: version.String,
		FileName: fileName.String, SizeBytes: size.Int64,
		DownloadURL: downloadURL.String, DigestSHA256: digest.String,
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

func (r Repository) rollbackSentSyncTask(ctx context.Context, nodeID, taskID, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = ?, lease_expires_at = NULL, updated_at = ?
		WHERE id = ? AND node_id = ? AND state = 'sent'`,
		nullable(message), now, taskID, nodeID)
	return err
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
	boundResult, err := bindTaskResultAsset(ctx, tx, session.NodeID, result)
	if err != nil {
		if err == sql.ErrNoRows {
			if err := r.updateSequence(session, seq); err != nil {
				return HeartbeatResult{}, err
			}
			ready := routingReady(ctx, tx, session.NodeID)
			if r.Logger != nil {
				r.Logger.Debug(ctx, "未知同步任务结果已忽略",
					slog.String("node_id", session.NodeID),
					slog.String("task_id", result.TaskID),
					slog.String("asset_id", result.AssetID),
					slog.String("result", result.Result))
			}
			return HeartbeatResult{
				AcceptedSequence: seq, ManagedState: managedState(ready),
				RoutingReady: ready,
			}, tx.Commit()
		}
		return HeartbeatResult{}, err
	}
	checkedResult, inventory, hasInventory, err := inspectSucceededSyncResult(ctx, tx, boundResult)
	if err != nil {
		return HeartbeatResult{}, err
	}
	taskState, attempts, retryAfter, err := r.applyTaskResult(ctx, tx, session.NodeID, checkedResult, nowValue)
	if err != nil {
		return HeartbeatResult{}, err
	}
	quarantined := false
	var pendingTaskNodes []string
	if hasInventory && taskState != "obsolete" && taskState != "stale_result" {
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
			nodes, err := publishVerifiedAsset(ctx, tx, inventory.AssetID, now)
			if err != nil {
				return HeartbeatResult{}, err
			}
			pendingTaskNodes = append(pendingTaskNodes, nodes...)
		}
	}
	if taskState == "succeeded" {
		if err := markDeletedInventory(ctx, tx, session.NodeID, checkedResult, now); err != nil {
			return HeartbeatResult{}, err
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
	if len(pendingTaskNodes) > 0 {
		r.runtime().NotifySyncTasks(pendingTaskNodes...)
	}
	if quarantined {
		return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(false), RoutingReady: false}, nil
	}
	if taskState == "retry_wait" && retryAfter != "" {
		r.scheduleSyncTaskRetryWake(session.NodeID, retryAfter)
	}
	return HeartbeatResult{
		AcceptedSequence: seq, ManagedState: managedState(ready),
		RoutingReady: ready, SyncTasksChanged: len(pendingTaskNodes) > 0,
	}, nil
}
