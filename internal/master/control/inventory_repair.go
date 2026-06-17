package control

import (
	"context"
	"database/sql"
)

type repairTarget struct {
	NodeID  string
	AssetID string
}

func markMissingInventory(ctx context.Context, tx *sql.Tx, nodeID, now string, reported map[string]bool) error {
	targets, err := loadRequiredAssetIDs(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	current, err := loadInventoryAt(ctx, tx, nodeID, now)
	if err != nil {
		return err
	}
	for _, assetID := range targets {
		if reported[assetID] || current[assetID] {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO node_inventory
			(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
			VALUES (?, ?, '', 0, ?, 'missing')
			ON CONFLICT(node_id, asset_id) DO UPDATE SET
			local_digest_sha256 = excluded.local_digest_sha256,
			size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
			state = excluded.state`, nodeID, assetID, now)
		if err != nil {
			return err
		}
	}
	return nil
}

func loadInventoryAt(ctx context.Context, tx *sql.Tx, nodeID, verifiedAt string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT asset_id FROM node_inventory
		WHERE node_id = ? AND verified_at = ?`, nodeID, verifiedAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return nil, err
		}
		out[assetID] = true
	}
	return out, rows.Err()
}

func createRepairTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	targets, err := loadRepairTargets(ctx, tx, nodeID)
	if err != nil {
		return 0, err
	}
	var generated int
	for _, target := range targets {
		inserted, err := insertRepairTask(ctx, tx, target, now)
		if err != nil {
			return generated, err
		}
		if inserted {
			generated++
		}
	}
	return generated, nil
}

func createKnownMissingRepairTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	targets, err := loadKnownMissingRepairTargets(ctx, tx, nodeID)
	if err != nil {
		return 0, err
	}
	var generated int
	for _, target := range targets {
		inserted, err := insertRepairTask(ctx, tx, target, now)
		if err != nil {
			return generated, err
		}
		if inserted {
			generated++
		}
	}
	return generated, nil
}

func clearSatisfiedDownloadTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'cancelled',
		error_message = '库存已验证，无需重新下载', completed_at = ?,
		retry_after = NULL, lease_expires_at = NULL, updated_at = ?
		WHERE node_id = ? AND task_type = 'asset_download'
		AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')
		AND asset_id IN (
			SELECT asset_id FROM node_inventory
			WHERE node_id = ? AND state = 'verified'
		)`, now, now, nodeID, nodeID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func resetMissingDownloadTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, retry_after = NULL, lease_expires_at = NULL,
		updated_at = ? WHERE node_id = ? AND task_type = 'asset_download'
		AND (
			state = 'retry_wait'
			OR (state IN ('sent', 'running') AND (
				lease_expires_at IS NULL OR lease_expires_at = '' OR lease_expires_at <= ?
			))
		)
		AND asset_id IN (
			SELECT ti.asset_id FROM target_inventory ti
			LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
			WHERE ti.node_id = ? AND ti.desired_state = 'required'
			AND (ni.asset_id IS NULL OR ni.state != 'verified')
		)`, now, nodeID, now, nodeID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func loadRepairTargets(ctx context.Context, tx *sql.Tx, nodeID string) ([]repairTarget, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ti.node_id, ti.asset_id
		FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		LEFT JOIN assets a ON a.id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified'
			OR ni.local_digest_sha256 != a.digest_sha256)`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []repairTarget
	for rows.Next() {
		var target repairTarget
		if err := rows.Scan(&target.NodeID, &target.AssetID); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func loadKnownMissingRepairTargets(ctx context.Context, tx *sql.Tx, nodeID string) ([]repairTarget, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ti.node_id, ti.asset_id
		FROM target_inventory ti
		JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		LEFT JOIN assets a ON a.id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.state != 'verified' OR ni.local_digest_sha256 != a.digest_sha256)`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []repairTarget
	for rows.Next() {
		var target repairTarget
		if err := rows.Scan(&target.NodeID, &target.AssetID); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func loadRequiredAssetIDs(ctx context.Context, tx *sql.Tx, nodeID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT asset_id FROM target_inventory
		WHERE node_id = ? AND desired_state = 'required'`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func insertRepairTask(ctx context.Context, tx *sql.Tx, target repairTarget, now string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = ? AND task_type = 'asset_download'
		AND state IN ('pending', 'sent', 'running', 'retry_wait')`,
		target.NodeID, target.AssetID).Scan(&exists)
	if err != nil || exists > 0 {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL, completed_at = NULL,
		lease_expires_at = NULL,
		updated_at = ? WHERE node_id = ? AND asset_id = ?
		AND task_type = 'asset_download' AND state IN ('failed', 'obsolete', 'cancelled')`,
		now, target.NodeID, target.AssetID)
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return true, nil
	}
	id, err := newID()
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES (?, ?, 'asset_download', ?, 'pending', ?, ?, ?)`,
		id, target.NodeID, target.AssetID, id, now, now)
	return err == nil, err
}
