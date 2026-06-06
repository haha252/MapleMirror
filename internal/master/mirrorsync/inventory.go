package mirrorsync

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/assetstate"
)

func rebuildTargetInventory(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	return assetstate.RebuildTargetInventory(ctx, tx, projectID, now)
}

func cancelObsoleteDownloadTasks(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	return assetstate.CancelObsoleteDownloadTasks(ctx, tx, projectID, now)
}

func generateTasks(ctx context.Context, tx *sql.Tx, now string) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ti.node_id, ti.asset_id
		FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified' OR ni.local_digest_sha256 !=
			(SELECT digest_sha256 FROM assets WHERE id = ti.asset_id))`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var generated int
	for rows.Next() {
		var nodeID, assetID string
		if err := rows.Scan(&nodeID, &assetID); err != nil {
			return generated, err
		}
		if err := insertTask(ctx, tx, nodeID, assetID, "asset_download", now); err != nil {
			return generated, err
		}
		generated++
	}
	return generated, rows.Err()
}

func insertTask(ctx context.Context, tx *sql.Tx, nodeID, assetID, taskType, now string) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = ? AND task_type = ?
		AND state IN ('pending', 'sent', 'running', 'retry_wait')`,
		nodeID, assetID, taskType).Scan(&exists)
	if err != nil || exists > 0 {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL, completed_at = NULL,
		updated_at = ? WHERE node_id = ? AND asset_id = ? AND task_type = ?
		AND state = 'failed'`, now, nodeID, assetID, taskType)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return nil
	}
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
		id, nodeID, taskType, assetID, id, now, now)
	return err
}
