package syncer

import "time"

func (e Executor) forceNextInventoryReport() error {
	if e.DB == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := e.DB.Exec(`INSERT OR IGNORE INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at)
		VALUES (1, 1, 0, ?)`, now); err != nil {
		return err
	}
	_, err := e.DB.Exec(`UPDATE inventory_report_cursor
		SET force_report_requested_at = COALESCE(NULLIF(force_report_requested_at, ''), ?)
		WHERE id = 1`, now)
	return err
}
