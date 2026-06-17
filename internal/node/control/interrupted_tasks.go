package control

import "time"

func (c Client) resetInterruptedLocalTasks() error {
	if c.DB == nil {
		return nil
	}
	tx, err := c.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT task_id, COALESCE(asset_id, '')
		FROM local_sync_tasks WHERE state = 'running'`)
	if err != nil {
		return err
	}
	type interruptedTask struct {
		taskID  string
		assetID string
	}
	var tasks []interruptedTask
	for rows.Next() {
		var task interruptedTask
		if err := rows.Scan(&task.taskID, &task.assetID); err != nil {
			_ = rows.Close()
			return err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(tasks) == 0 {
		return tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, task := range tasks {
		if _, err := tx.Exec(`INSERT INTO pending_sync_task_results
			(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at, reported_at)
			VALUES (?, ?, 'temporary_error', NULL, 0, ?, ?, NULL)
			ON CONFLICT(task_id) DO UPDATE SET asset_id = excluded.asset_id,
			result = excluded.result, local_digest_sha256 = NULL, size_bytes = 0,
			message = excluded.message, created_at = excluded.created_at, reported_at = NULL`,
			task.taskID, nullableString(task.assetID),
			"control connection restarted while task was running", now); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE local_sync_tasks SET state = 'interrupted',
		error_message = 'control connection restarted while task was running',
		updated_at = ? WHERE state = 'running'`, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}
