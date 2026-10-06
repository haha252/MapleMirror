package assignment

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

func GenerateMissingTasks(ctx context.Context, tx *sql.Tx, now string) (int, error) {
	return generateTasks(ctx, tx, now, "")
}

func GenerateNodeTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	return generateTasks(ctx, tx, now, nodeID)
}

func GenerateNodeDeleteTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) (int, error) {
	count, err := generateDeleteTasks(ctx, tx, now, nodeID, "")
	if err == nil {
		err = InvalidateReadiness(ctx, tx, nodeID, now)
	}
	return count, err
}

func GenerateDeleteTasks(ctx context.Context, tx *sql.Tx, now string) (int, error) {
	count, err := generateDeleteTasks(ctx, tx, now, "", "")
	if err == nil {
		err = InvalidateReadiness(ctx, tx, "", now)
	}
	return count, err
}

func GenerateProjectDeleteTasks(ctx context.Context, tx *sql.Tx, projectID, now string) (int, error) {
	count, err := generateDeleteTasks(ctx, tx, now, "", projectID)
	if err == nil {
		err = reconcileReadiness(ctx, tx, "", projectID, now, false)
	}
	return count, err
}

func generateTasks(ctx context.Context, tx *sql.Tx, now, nodeID string) (int, error) {
	downloads, err := generateDownloadTasks(ctx, tx, now, nodeID)
	if err != nil {
		return downloads, err
	}
	deletes, err := generateDeleteTasks(ctx, tx, now, nodeID, "")
	if err == nil {
		err = InvalidateReadiness(ctx, tx, nodeID, now)
	}
	return downloads + deletes, err
}

func generateDownloadTasks(ctx context.Context, tx *sql.Tx, now, nodeID string) (int, error) {
	query := `SELECT ti.node_id, ti.asset_id
		FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified' OR ni.local_digest_sha256 !=
			(SELECT digest_sha256 FROM assets WHERE id = ti.asset_id))`
	args := []any{}
	if nodeID != "" {
		query += ` AND ti.node_id = ?`
		args = append(args, nodeID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var generated int
	for rows.Next() {
		var targetNodeID, assetID string
		if err := rows.Scan(&targetNodeID, &assetID); err != nil {
			return generated, err
		}
		inserted, err := insertDownloadTask(ctx, tx, targetNodeID, assetID, now)
		if err != nil {
			return generated, err
		}
		if inserted {
			generated++
		}
	}
	return generated, rows.Err()
}

func generateDeleteTasks(ctx context.Context, tx *sql.Tx, now, nodeID, projectID string) (int, error) {
	query := `SELECT ti.node_id, ti.asset_id
		FROM target_inventory ti
		JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.desired_state = 'remove' AND ni.state = 'verified'`
	args := []any{}
	if nodeID != "" {
		query += ` AND ti.node_id = ?`
		args = append(args, nodeID)
	}
	if projectID != "" {
		query += ` AND ti.asset_id IN (
			SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
			WHERE r.project_id = ?)`
		args = append(args, projectID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var generated int
	for rows.Next() {
		var targetNodeID, assetID string
		if err := rows.Scan(&targetNodeID, &assetID); err != nil {
			return generated, err
		}
		inserted, err := insertDeleteTask(ctx, tx, targetNodeID, assetID, now)
		if err != nil {
			return generated, err
		}
		if inserted {
			generated++
		}
	}
	return generated, rows.Err()
}

func insertDownloadTask(ctx context.Context, tx *sql.Tx, nodeID, assetID, now string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = ? AND task_type = 'asset_download'
		AND (state IN ('pending', 'sent', 'running', 'retry_wait') OR
		 (state='cancelled' AND error_message='管理员取消'))`,
		nodeID, assetID).Scan(&exists)
	if err != nil || exists > 0 {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL, completed_at = NULL,
		lease_expires_at = NULL, updated_at = ? WHERE node_id = ? AND asset_id = ?
		AND task_type = 'asset_download' AND state IN ('failed', 'obsolete', 'cancelled')`,
		now, nodeID, assetID)
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return true, nil
	}
	id := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES (?, ?, 'asset_download', ?, 'pending', ?, ?, ?)`,
		id, nodeID, assetID, id, now, now)
	return err == nil, err
}

func insertDeleteTask(ctx context.Context, tx *sql.Tx, nodeID, assetID, now string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = ? AND task_type = 'asset_delete'
		AND state IN ('pending', 'sent', 'running', 'retry_wait')`,
		nodeID, assetID).Scan(&exists)
	if err != nil || exists > 0 {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL, completed_at = NULL,
		lease_expires_at = NULL, updated_at = ? WHERE node_id = ? AND asset_id = ?
		AND task_type = 'asset_delete'
		AND state IN ('failed', 'obsolete', 'cancelled', 'succeeded')`,
		now, nodeID, assetID)
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return true, nil
	}
	id := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES (?, ?, 'asset_delete', ?, 'pending', ?, ?, ?)`,
		id, nodeID, assetID, id, now, now)
	return err == nil, err
}
