package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/protocol"
)

func inspectSucceededSyncResult(ctx context.Context, tx *sql.Tx,
	result protocol.SyncTaskResult) (protocol.SyncTaskResult, inventoryAcceptResult, bool, error) {
	if result.Result != "succeeded" || result.AssetID == "" {
		return result, inventoryAcceptResult{}, false, nil
	}
	var taskType string
	err := tx.QueryRowContext(ctx, `SELECT task_type FROM node_tasks
		WHERE id = ?`, result.TaskID).Scan(&taskType)
	if err == sql.ErrNoRows || taskType != "asset_download" {
		return result, inventoryAcceptResult{}, false, nil
	}
	if err != nil {
		return result, inventoryAcceptResult{}, false, err
	}
	inventory := inventoryAcceptResult{
		AssetID:     result.AssetID,
		LocalDigest: result.LocalDigestSHA256,
		LocalSize:   result.SizeBytes,
		State:       "verified",
	}
	err = tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, result.AssetID).Scan(&inventory.ExpectedDigest, &inventory.ExpectedSize)
	if err == sql.ErrNoRows {
		return result, inventoryAcceptResult{}, false, nil
	}
	if err != nil {
		return result, inventoryAcceptResult{}, false, err
	}
	inventory.PublicAsset = publicCandidateAsset(ctx, tx, result.AssetID)
	if result.LocalDigestSHA256 == inventory.ExpectedDigest && result.SizeBytes == inventory.ExpectedSize {
		return result, inventory, true, nil
	}
	inventory.State = "mismatch"
	checked := result
	checked.Result = "digest_mismatch"
	if result.SizeBytes != inventory.ExpectedSize {
		checked.Result = "size_mismatch"
	}
	if checked.Message == "" {
		checked.Message = "节点回报成功但本地资产摘要或大小与期望不一致"
	}
	return checked, inventory, true, nil
}

func upsertSyncResultInventory(ctx context.Context, tx *sql.Tx, nodeID string,
	inventory inventoryAcceptResult, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
		state = excluded.state`,
		nodeID, inventory.AssetID, inventory.LocalDigest, inventory.LocalSize, now, inventory.State)
	return err
}

func markDeletedInventory(ctx context.Context, tx *sql.Tx, nodeID string,
	result protocol.SyncTaskResult, now string) error {
	var taskType string
	err := tx.QueryRowContext(ctx, `SELECT task_type FROM node_tasks
		WHERE id = ? AND node_id = ?`, result.TaskID, nodeID).Scan(&taskType)
	if err != nil || taskType != "asset_delete" || result.AssetID == "" {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_inventory SET state = 'removed',
		verified_at = ?, local_digest_sha256 = '', size_bytes = 0
		WHERE node_id = ? AND asset_id = ?`, now, nodeID, result.AssetID)
	return err
}
