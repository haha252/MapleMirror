package mirrorsync

import (
	"context"
	"database/sql"
)

func rebuildTargetInventory(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT n.id, a.id, 'required', ?
		FROM nodes n JOIN releases r ON r.project_id = ?
		JOIN assets a ON a.release_id = r.id
		WHERE n.state != 'disabled' AND r.selected = 1 AND a.service_state = 'candidate'`,
		now, projectID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE asset_id IN (
		SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND (r.selected = 0 OR a.service_state != 'candidate'))`,
		now, projectID)
	return err
}

func generateTasks(ctx context.Context, tx *sql.Tx, now string) error {
	rows, err := tx.QueryContext(ctx, `SELECT ti.node_id, ti.asset_id
		FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified' OR ni.local_digest_sha256 !=
			(SELECT digest_sha256 FROM assets WHERE id = ti.asset_id))`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, assetID string
		if err := rows.Scan(&nodeID, &assetID); err != nil {
			return err
		}
		if err := insertTask(ctx, tx, nodeID, assetID, "asset_download", now); err != nil {
			return err
		}
	}
	return rows.Err()
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
