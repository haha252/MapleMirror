package mirrorsync

import (
	"context"
	"database/sql"
	"mirror-server/internal/master/assignment"
)

func updateTask(ctx context.Context, db *sql.DB, nodeID, taskID, state, msg string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = ?,
		error_message = ?, updated_at = ?, lease_expires_at=NULL,retry_after=NULL WHERE id = ? AND node_id = ?`,
		state, nullable(msg), nowText(), taskID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if err := assignment.ReconcileReadiness(ctx, tx, nodeID, nowText()); err != nil {
		return err
	}
	return tx.Commit()
}
