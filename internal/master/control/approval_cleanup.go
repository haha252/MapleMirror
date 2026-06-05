package control

import (
	"context"
	"database/sql"
)

func deleteQuarantinedNodesByName(ctx context.Context, tx *sql.Tx, name, newNodeID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM nodes n
		WHERE n.public_name = ? AND n.id != ? AND n.state = 'disabled'
		AND EXISTS(
			SELECT 1 FROM admin_audit_events a
			WHERE a.operation = 'node.security_quarantine'
			AND a.target_type = 'node' AND a.target_id = n.id
		)
		ORDER BY n.created_at`, name, newNodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodeIDs []string
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		nodeIDs = append(nodeIDs, nodeID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, nodeID := range nodeIDs {
		if err := deleteNodeData(ctx, tx, nodeID); err != nil {
			return nil, err
		}
	}
	return nodeIDs, nil
}

func deleteNodeData(ctx context.Context, tx *sql.Tx, nodeID string) error {
	statements := []string{
		`DELETE FROM traffic_events WHERE node_id = ?`,
		`DELETE FROM traffic_reservations WHERE authorization_id IN (
			SELECT id FROM download_authorizations WHERE node_id = ?
		)`,
		`DELETE FROM download_authorizations WHERE node_id = ?`,
		`DELETE FROM daily_node_traffic_stats WHERE node_id = ?`,
		`DELETE FROM node_availability_samples WHERE node_id = ?`,
		`DELETE FROM node_pressure_reports WHERE node_id = ?`,
		`DELETE FROM node_inventory_reports WHERE node_id = ?`,
		`DELETE FROM node_heartbeats WHERE node_id = ?`,
		`DELETE FROM node_control_sessions WHERE node_id = ?`,
		`DELETE FROM node_tasks WHERE node_id = ?`,
		`DELETE FROM node_inventory WHERE node_id = ?`,
		`DELETE FROM target_inventory WHERE node_id = ?`,
		`DELETE FROM node_certificates WHERE node_id = ?`,
		`DELETE FROM nodes WHERE id = ?`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt, nodeID); err != nil {
			return err
		}
	}
	return nil
}
