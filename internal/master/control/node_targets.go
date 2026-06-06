package control

import (
	"context"
	"database/sql"
)

func seedNodeTargets(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	rows, err := tx.QueryContext(ctx, `SELECT a.id
		FROM assets a JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		WHERE p.enabled = 1 AND r.selected = 1
		AND a.service_state IN ('candidate', 'pending')
		ORDER BY r.published_at DESC, a.size_bytes, a.file_name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			return err
		}
		if err := seedNodeTarget(ctx, tx, nodeID, assetID, now); err != nil {
			return err
		}
	}
	return rows.Err()
}

func seedNodeTarget(ctx context.Context, tx *sql.Tx, nodeID, assetID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, ?, 'required', ?)
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		desired_state = 'required', updated_at = excluded.updated_at`,
		nodeID, assetID, now)
	if err != nil {
		return err
	}
	var verified int
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM node_inventory ni JOIN assets a ON a.id = ni.asset_id
		WHERE ni.node_id = ? AND ni.asset_id = ? AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256
		AND ni.size_bytes = a.size_bytes
	)`, nodeID, assetID).Scan(&verified)
	if err != nil || verified == 1 {
		return err
	}
	return insertNodeDownloadTask(ctx, tx, nodeID, assetID, now)
}

func insertNodeDownloadTask(ctx context.Context, tx *sql.Tx, nodeID, assetID, now string) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = ? AND task_type = 'asset_download'
		AND state IN ('pending', 'sent', 'running', 'retry_wait')`,
		nodeID, assetID).Scan(&exists)
	if err != nil || exists > 0 {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL,
		completed_at = NULL, lease_expires_at = NULL, updated_at = ?
		WHERE node_id = ? AND asset_id = ? AND task_type = 'asset_download'
		AND state IN ('failed', 'obsolete', 'cancelled')`,
		now, nodeID, assetID)
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
		VALUES (?, ?, 'asset_download', ?, 'pending', ?, ?, ?)`,
		id, nodeID, assetID, id, now, now)
	return err
}
