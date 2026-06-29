package mirrorsync

import (
	"context"
	"database/sql"
)

func targetInventoryCounts(ctx context.Context, db *sql.DB, nodeID string) (required, missing int) {
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN ni.asset_id IS NULL OR ni.state != 'verified'
			THEN 1 ELSE 0 END), 0)
		FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'`, nodeID).
		Scan(&required, &missing)
	return required, missing
}

func nodeInventoryCounts(ctx context.Context, db *sql.DB, nodeID string) (verified, mismatched int) {
	rows, err := db.QueryContext(ctx, `SELECT state, COUNT(*)
		FROM node_inventory WHERE node_id = ? AND state IN ('verified', 'mismatch')
		GROUP BY state`, nodeID)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return verified, mismatched
		}
		switch state {
		case "verified":
			verified = n
		case "mismatch":
			mismatched = n
		}
	}
	return verified, mismatched
}

func syncTaskCounts(ctx context.Context, db *sql.DB, nodeID string) map[string]int {
	out := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT state, COUNT(*)
		FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')
		GROUP BY state`, nodeID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return out
		}
		out[state] = n
	}
	return out
}
