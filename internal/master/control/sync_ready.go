package control

import (
	"context"
	"database/sql"
	"log/slog"
)

func (r Repository) reconcileNodeReady(ctx context.Context, tx *sql.Tx, nodeID, now string) (bool, error) {
	var previousReady int
	var nodeState string
	var lastHeartbeat string
	if err := tx.QueryRowContext(ctx, `SELECT routing_ready, state,
		COALESCE(last_heartbeat_at, '') FROM nodes WHERE id = ?`, nodeID).
		Scan(&previousReady, &nodeState, &lastHeartbeat); err != nil {
		return false, err
	}
	var missing, running int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID).Scan(&missing); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		nodeID).Scan(&running); err != nil {
		return false, err
	}
	ready := 0
	if nodeState != "disabled" && nodeState != "offline" && lastHeartbeat != "" &&
		missing == 0 && running == 0 {
		ready = 1
	}
	var err error
	if previousReady != ready {
		_, err = tx.ExecContext(ctx, `UPDATE nodes SET routing_ready = ?,
			updated_at = ? WHERE id = ?`, ready, now, nodeID)
	}
	if err == nil && r.Logger != nil && previousReady != ready {
		r.Logger.Debug(ctx, "节点同步就绪状态已更新",
			slog.String("node_id", nodeID),
			slog.Bool("routing_ready", ready == 1),
			slog.Int("missing_targets", missing),
			slog.Int("running_tasks", running),
			slog.String("connection_state", nodeState))
	}
	return ready == 1, err
}

func readySnapshot(ctx context.Context, tx *sql.Tx, nodeID string) (missing, running int, ready bool) {
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID).Scan(&missing)
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`,
		nodeID).Scan(&running)
	var readyInt int
	_ = tx.QueryRowContext(ctx, `SELECT routing_ready FROM nodes WHERE id = ?`, nodeID).Scan(&readyInt)
	return missing, running, readyInt == 1
}
