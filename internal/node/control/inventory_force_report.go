package control

import (
	"database/sql"
	"time"
)

func forceInventoryReportDue(tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT OR IGNORE INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at)
		VALUES (1, 1, 0, ?)`, now); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE inventory_report_cursor
		SET force_report_requested_at = COALESCE(NULLIF(force_report_requested_at, ''), ?)
		WHERE id = 1`, now)
	return err
}

func localTaskType(tx *sql.Tx, taskID string) (string, error) {
	var taskType string
	err := tx.QueryRow(`SELECT task_type FROM local_sync_tasks WHERE task_id = ?`, taskID).
		Scan(&taskType)
	return taskType, err
}
