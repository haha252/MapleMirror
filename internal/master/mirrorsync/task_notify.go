package mirrorsync

import "context"

func (s Store) notifyPendingTaskNodes(ctx context.Context, query string, args ...any) {
	if s.Runtime == nil {
		return
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err == nil {
			s.Runtime.NotifySyncTasks(nodeID)
		}
	}
}

func (s Store) NotifyProjectTaskNodes(ctx context.Context, projectID string) {
	s.notifyPendingTaskNodes(ctx, `SELECT DISTINCT t.node_id
		FROM node_tasks t JOIN assets a ON a.id = t.asset_id
		JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ?
		AND t.state IN ('pending', 'retry_wait')`, projectID)
}
