package mirrorsync

import "context"

// SyncStatuses reads the live counts for all nodes in one query. Inventory and
// session state come from the same runtime used by the single-node diagnosis.
func (s Store) SyncStatuses(ctx context.Context) ([]SyncStatus, error) {
	rows, err := s.DB.QueryContext(ctx, `WITH targets AS (
		SELECT ti.node_id, COUNT(*) AS required,
			SUM(CASE WHEN ni.asset_id IS NULL OR ni.state != 'verified' THEN 1 ELSE 0 END) AS missing
		FROM target_inventory ti LEFT JOIN node_inventory ni
			ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.desired_state = 'required' GROUP BY ti.node_id
	), inventory AS (
		SELECT node_id, SUM(state = 'verified') AS verified, SUM(state = 'mismatch') AS mismatched
		FROM node_inventory WHERE state IN ('verified', 'mismatch') GROUP BY node_id
	), tasks AS (
		SELECT node_id, SUM(state = 'pending') AS pending, SUM(state = 'sent') AS sent,
			SUM(state = 'running') AS running, SUM(state = 'retry_wait') AS retry,
			SUM(state = 'failed') AS failed
		FROM node_tasks WHERE state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')
		GROUP BY node_id
	)
	SELECT n.id, n.state, n.routing_ready, COALESCE(n.last_heartbeat_at, ''),
		COALESCE(t.required, 0), COALESCE(t.missing, 0), COALESCE(i.verified, 0),
		COALESCE(i.mismatched, 0), COALESCE(k.pending, 0), COALESCE(k.sent, 0),
		COALESCE(k.running, 0), COALESCE(k.retry, 0), COALESCE(k.failed, 0)
	FROM nodes n LEFT JOIN targets t ON t.node_id = n.id
	LEFT JOIN inventory i ON i.node_id = n.id LEFT JOIN tasks k ON k.node_id = n.id
	ORDER BY n.created_at, n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SyncStatus{}
	for rows.Next() {
		var item SyncStatus
		var ready int
		if err := rows.Scan(&item.NodeID, &item.ConnectionState, &ready, &item.LastHeartbeatAt,
			&item.RequiredAssets, &item.MissingAssets, &item.VerifiedAssets, &item.MismatchedAssets,
			&item.PendingTasks, &item.SentTasks, &item.RunningTasks, &item.RetryWaitTasks, &item.FailedTasks); err != nil {
			return nil, err
		}
		item.RoutingReady = ready == 1
		item.OutstandingTasks = item.PendingTasks + item.SentTasks + item.RunningTasks + item.RetryWaitTasks + item.FailedTasks
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		item := &items[i]
		if s.Runtime != nil {
			item.LatestInventoryRevision, item.LatestInventoryComplete, item.HasInventoryReport = s.Runtime.LatestInventoryState(item.NodeID)
			item.ActiveControlSession = s.Runtime.ActiveSession(item.NodeID)
		} else {
			var complete int
			_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(revision, 0), COALESCE(complete, 0)
				FROM node_inventory_reports WHERE node_id = ? ORDER BY reported_at DESC LIMIT 1`, item.NodeID).
				Scan(&item.LatestInventoryRevision, &complete)
			item.LatestInventoryComplete = complete == 1
			item.HasInventoryReport = item.LatestInventoryRevision > 0
			item.ActiveControlSession = exists(ctx, s.DB, `SELECT 1 FROM node_control_sessions
				WHERE node_id = ? AND disconnected_at IS NULL`, item.NodeID)
		}
		item.SyncPhase = syncPhase(*item)
		item.RoutingReadyReason, item.RoutingReadyDetail = syncStatusReason(*item)
	}
	return items, nil
}
