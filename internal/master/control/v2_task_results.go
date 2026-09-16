package control

import (
	"context"
	"database/sql"
	"time"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) acceptV2SyncResult(ctx context.Context, session Session, result protocolv2.SyncResult) (bool, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var currentAttempt string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(attempt_id,'') FROM node_tasks WHERE id=? AND node_id=?`,
		result.TaskID, session.NodeID).Scan(&currentAttempt); err != nil {
		if err == sql.ErrNoRows {
			return true, tx.Commit()
		}
		return false, err
	}
	if currentAttempt != result.AttemptID {
		return true, tx.Commit()
	}
	legacy := protocol.SyncTaskResult{TaskID: result.TaskID, AssetID: result.AssetID, Result: result.Result,
		LocalDigestSHA256: result.LocalDigestSHA256, SizeBytes: result.SizeBytes, Message: result.Message,
		PeerFallbackAttempted: true}
	nowValue := time.Now().UTC()
	now := nowValue.Format(time.RFC3339Nano)
	bound, err := bindTaskResultAsset(ctx, tx, session.NodeID, legacy)
	if err != nil {
		if err == sql.ErrNoRows {
			return true, tx.Commit()
		}
		return false, err
	}
	checked, inventory, hasInventory, err := inspectSucceededSyncResult(ctx, tx, bound)
	if err != nil {
		return false, err
	}
	taskState, _, retryAfter, err := r.applyTaskResult(ctx, tx, session.NodeID, checked, nowValue)
	if err != nil {
		return false, err
	}
	var pendingNodes []string
	if hasInventory && taskState != "obsolete" && taskState != "stale_result" {
		if err := upsertSyncResultInventory(ctx, tx, session.NodeID, inventory, now); err != nil {
			return false, err
		}
		if inventory.State == "verified" {
			pendingNodes, err = publishVerifiedAsset(ctx, tx, inventory.AssetID, now)
			if err != nil {
				return false, err
			}
		}
	}
	if taskState == "succeeded" {
		if err := markDeletedInventory(ctx, tx, session.NodeID, checked, now); err != nil {
			return false, err
		}
	}
	if _, err := r.reconcileNodeReady(ctx, tx, session.NodeID, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if len(pendingNodes) > 0 {
		r.runtime().NotifySyncTasks(pendingNodes...)
	}
	if taskState == "retry_wait" && retryAfter != "" {
		r.scheduleSyncTaskRetryWake(session.NodeID, retryAfter)
	}
	return true, nil
}
