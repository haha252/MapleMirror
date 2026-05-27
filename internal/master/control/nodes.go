package control

import (
	"context"
	"time"
)

type NodeSummary struct {
	NodeID        string `json:"node_id"`
	PublicName    string `json:"public_name"`
	State         string `json:"state"`
	RoutingReady  bool   `json:"routing_ready"`
	LastHeartbeat string `json:"last_heartbeat_at,omitempty"`
}

func (r Repository) ListNodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, COALESCE(last_heartbeat_at, '') FROM nodes ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []NodeSummary
	for rows.Next() {
		var item NodeSummary
		var ready int
		if err := rows.Scan(&item.NodeID, &item.PublicName, &item.State, &ready, &item.LastHeartbeat); err != nil {
			return nil, err
		}
		item.RoutingReady = false
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) DisableNode(ctx context.Context, nodeID, requestID, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET state = 'disabled',
		routing_ready = 0, updated_at = ? WHERE id = ?`, now, nodeID)
	if err != nil {
		return err
	}
	_, _ = r.DB.ExecContext(ctx, `UPDATE node_certificates SET status = 'revoked',
		revoked_at = ? WHERE node_id = ? AND status = 'active'`, now, nodeID)
	_, _ = r.DB.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = '管理员禁用' WHERE node_id = ? AND disconnected_at IS NULL`, now, nodeID)
	return r.Audit(ctx, "node.disable", "node", nodeID, "success", requestID, reason, "")
}

func (r Repository) EnableNode(ctx context.Context, nodeID, requestID, admin string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET state = 'syncing',
		routing_ready = 0, updated_at = ? WHERE id = ? AND state = 'disabled'`, now, nodeID)
	if err != nil {
		return err
	}
	return r.Audit(ctx, "node.enable", "node", nodeID, "success", requestID, "节点已启用并等待心跳", admin)
}

func (r Repository) SyncReset(ctx context.Context, nodeID, requestID, admin string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET state = 'syncing',
		routing_ready = 0, updated_at = ? WHERE id = ? AND state != 'disabled'`, now, nodeID)
	if err != nil {
		return err
	}
	return r.Audit(ctx, "node.sync_reset", "node", nodeID, "success", requestID, "同步状态已重置", admin)
}

func (r Repository) Audit(ctx context.Context, op, targetType, targetID, result, requestID, summary, admin string) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO admin_audit_events
		(id, operation, target_type, target_id, result, request_id, created_at,
		admin_identity, details_summary) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mustID(), op, targetType, targetID, result, requestID,
		time.Now().UTC().Format(time.RFC3339Nano), admin, summary)
	return err
}

func (r Repository) LatestHeartbeat(ctx context.Context, nodeID string) (map[string]any, error) {
	var state, reported string
	var pressure float64
	var active, free int64
	err := r.DB.QueryRowContext(ctx, `SELECT state, pressure_ratio,
		active_downloads, free_bytes, reported_at FROM node_heartbeats
		WHERE node_id = ? ORDER BY reported_at DESC LIMIT 1`, nodeID).
		Scan(&state, &pressure, &active, &free, &reported)
	return map[string]any{"state": state, "pressure_ratio": pressure,
		"active_downloads": active, "free_bytes": free, "reported_at": reported}, err
}

func (r Repository) LatestInventoryReport(ctx context.Context, nodeID string) (map[string]any, error) {
	var revision, complete, count int
	var result, requestID, reported string
	err := r.DB.QueryRowContext(ctx, `SELECT revision, complete, item_count,
		result, request_id, reported_at FROM node_inventory_reports
		WHERE node_id = ? ORDER BY reported_at DESC LIMIT 1`, nodeID).
		Scan(&revision, &complete, &count, &result, &requestID, &reported)
	return map[string]any{"revision": revision, "complete": complete == 1,
		"item_count": count, "result": result, "request_id": requestID,
		"reported_at": reported}, err
}

func (r Repository) LatestPressureReport(ctx context.Context, nodeID string) (map[string]any, error) {
	var pressure float64
	var active, free int64
	var requestID, reported string
	err := r.DB.QueryRowContext(ctx, `SELECT pressure_ratio, active_downloads,
		free_bytes, request_id, reported_at FROM node_pressure_reports
		WHERE node_id = ? ORDER BY reported_at DESC LIMIT 1`, nodeID).
		Scan(&pressure, &active, &free, &requestID, &reported)
	return map[string]any{"pressure_ratio": pressure, "active_downloads": active,
		"free_bytes": free, "request_id": requestID, "reported_at": reported}, err
}
