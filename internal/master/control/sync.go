package control

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/protocol"
)

const maxSyncFallbackSources = 3

func (r Repository) NextSyncTask(ctx context.Context, nodeID string) (protocol.SyncTask, bool, error) {
	var task protocol.SyncTask
	var attempts int
	var retryAfter string
	err := r.DB.QueryRowContext(ctx, `SELECT t.id, t.task_type, a.id, r.project_id,
		r.tag_name, a.file_name, a.size_bytes, a.source_url, a.digest_sha256, COALESCE(t.attempts, 0),
		COALESCE(t.retry_after, '')
		FROM node_tasks t LEFT JOIN assets a ON a.id = t.asset_id
		LEFT JOIN releases r ON r.id = a.release_id
		WHERE t.node_id = ?
		AND (
			t.state = 'pending'
			OR (t.state = 'retry_wait' AND (t.retry_after IS NULL OR t.retry_after = '' OR t.retry_after <= ?))
		)
		ORDER BY t.created_at LIMIT 1`, nodeID, time.Now().UTC().Format(time.RFC3339Nano)).
		Scan(&task.TaskID, &task.TaskType, &task.Asset.AssetID,
			&task.Asset.ProjectID, &task.Asset.Version, &task.Asset.FileName, &task.Asset.SizeBytes,
			&task.Asset.DownloadURL, &task.Asset.DigestSHA256,
			&attempts, &retryAfter)
	if err == sql.ErrNoRows {
		return protocol.SyncTask{}, false, nil
	}
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	if task.TaskType == "asset_download" {
		task.FallbackSources = r.syncFallbackSources(ctx, nodeID, task)
	}
	_, err = r.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'sent',
		updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339Nano), task.TaskID)
	if err == nil && r.Logger != nil {
		r.Logger.Debug(ctx, "同步任务可派发",
			slog.String("node_id", nodeID),
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("asset_id", task.Asset.AssetID),
			slog.Int("attempts", attempts),
			slog.String("retry_after", retryAfter))
	}
	return task, true, err
}

func (r Repository) syncFallbackSources(ctx context.Context, targetNodeID string, task protocol.SyncTask) []protocol.SyncFallbackSource {
	rows, err := r.DB.QueryContext(ctx, `SELECT n.id, n.public_name, n.public_download_base_url
		FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
		JOIN assets a ON a.id = ni.asset_id
		WHERE ni.asset_id = ? AND ni.node_id != ? AND n.state != 'disabled'
		AND n.state != 'offline' AND n.last_heartbeat_at IS NOT NULL
		AND n.last_heartbeat_at != '' AND n.public_download_base_url != '' AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
		ORDER BY n.public_name, n.id LIMIT ?`,
		task.Asset.AssetID, targetNodeID, maxSyncFallbackSources)
	if err != nil {
		return nil
	}
	defer rows.Close()
	expires := time.Now().UTC().Add(r.replicationTokenTTL()).Format(time.RFC3339Nano)
	out := make([]protocol.SyncFallbackSource, 0, maxSyncFallbackSources)
	for rows.Next() {
		var nodeID, nodeName, baseURL string
		if err := rows.Scan(&nodeID, &nodeName, &baseURL); err != nil {
			return out
		}
		token, err := r.ReplicationSigner.SignReplication(downloadtoken.ReplicationClaims{
			AssetID: task.Asset.AssetID, SourceNodeID: nodeID, TargetNodeID: targetNodeID,
			ExpiresAt: expires, RequestID: task.TaskID, TaskID: task.TaskID,
		})
		if err != nil {
			continue
		}
		out = append(out, protocol.SyncFallbackSource{
			NodeID: nodeID, NodeName: nodeName,
			DownloadURL: joinReplicationURL(baseURL, task.Asset.AssetID),
			Token:       token,
		})
	}
	return out
}

func (r Repository) replicationTokenTTL() time.Duration {
	if r.ReplicationTokenTTL > 0 {
		return r.ReplicationTokenTTL
	}
	return 15 * time.Minute
}

func joinReplicationURL(baseURL, assetID string) string {
	return strings.TrimRight(baseURL, "/") + "/internal/replication/" + url.PathEscape(assetID)
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
	taskState, attempts, retryAfter, err := r.applyTaskResult(ctx, tx, session.NodeID, result, nowValue)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if result.Result == "succeeded" && result.AssetID != "" {
		if err := upsertVerifiedInventory(ctx, tx, session.NodeID, result, now); err != nil {
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
			slog.String("task_id", result.TaskID),
			slog.String("asset_id", result.AssetID),
			slog.String("result", result.Result),
			slog.String("task_state", taskState),
			slog.Int("attempts", attempts),
			slog.String("retry_after", retryAfter),
			slog.String("message", result.Message))
	}
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, finish(tx, err)
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

func (r Repository) reconcileNodeReady(ctx context.Context, tx *sql.Tx, nodeID, now string) (bool, error) {
	var previousReady int
	if err := tx.QueryRowContext(ctx, `SELECT routing_ready FROM nodes WHERE id = ?`, nodeID).Scan(&previousReady); err != nil {
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
	state := "syncing"
	if missing == 0 && running == 0 {
		ready = 1
		state = "ready"
	}
	_, err := tx.ExecContext(ctx, `UPDATE nodes SET routing_ready = ?,
		state = CASE WHEN state = 'disabled' THEN state ELSE ? END,
		updated_at = ? WHERE id = ?`, ready, state, now, nodeID)
	if err == nil && r.Logger != nil && previousReady != ready {
		r.Logger.Debug(ctx, "节点同步就绪状态已更新",
			slog.String("node_id", nodeID),
			slog.Bool("routing_ready", ready == 1),
			slog.Int("missing_targets", missing),
			slog.Int("running_tasks", running),
			slog.String("state", state))
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
