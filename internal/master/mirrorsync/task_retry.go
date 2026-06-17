package mirrorsync

import (
	"context"
	"database/sql"
)

func (s Store) RetryTask(ctx context.Context, nodeID, taskID string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL, updated_at = ?
		WHERE id = ? AND node_id = ?
		AND task_type = 'asset_download'
		AND state IN ('failed', 'retry_wait')
		AND asset_id IS NOT NULL
		AND EXISTS (
			SELECT 1 FROM target_inventory ti
			JOIN assets a ON a.id = node_tasks.asset_id
				AND a.service_state IN ('candidate', 'pending', 'active')
			JOIN releases r ON r.id = a.release_id AND r.selected = 1
			JOIN projects p ON p.id = r.project_id AND p.enabled = 1
			WHERE ti.node_id = node_tasks.node_id
			AND ti.asset_id = node_tasks.asset_id
			AND ti.desired_state = 'required'
		)`, nowText(), taskID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
