package control

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrInvalidDownloadPriority = errors.New("invalid node download priority")

type NodeSummary struct {
	NodeID             string `json:"node_id"`
	PublicName         string `json:"public_name"`
	State              string `json:"state"`
	ConnectionState    string `json:"connection_state"`
	RoutingReady       bool   `json:"routing_ready"`
	TargetBandwidthBPS int64  `json:"target_bandwidth_bps"`
	DownloadPriority   int    `json:"download_priority"`
	MaxMirrorProjects  int    `json:"max_mirror_projects"`
	AssignmentMode     string `json:"project_assignment_mode"`
	LastHeartbeat      string `json:"last_heartbeat_at,omitempty"`
}

func (r Repository) ListNodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, target_bandwidth_bps, download_priority, max_mirror_projects,
		project_assignment_mode, COALESCE(last_heartbeat_at, '')
		FROM nodes ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []NodeSummary
	for rows.Next() {
		var item NodeSummary
		var ready int
		if err := rows.Scan(&item.NodeID, &item.PublicName, &item.State,
			&ready, &item.TargetBandwidthBPS, &item.DownloadPriority, &item.MaxMirrorProjects,
			&item.AssignmentMode, &item.LastHeartbeat); err != nil {
			return nil, err
		}
		item.ConnectionState = item.State
		item.RoutingReady = ready == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) UpdateNodeDownloadPriority(ctx context.Context, nodeID string, priority int) error {
	if priority < 0 || priority > 100 {
		return ErrInvalidDownloadPriority
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := r.DB.ExecContext(ctx, `UPDATE nodes SET download_priority = ?,
		updated_at = ? WHERE id = ?`, priority, now, nodeID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r Repository) DisableNode(ctx context.Context, nodeID, requestID, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := r.DB.ExecContext(ctx, `UPDATE nodes SET state = 'disabled',
		routing_ready = 0, updated_at = ? WHERE id = ?`, now, nodeID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return r.Audit(ctx, "node.disable", "node", nodeID, "success", requestID, reason, "")
}

func (r Repository) EnableNode(ctx context.Context, nodeID, requestID, admin string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM nodes WHERE id = ?`, nodeID).Scan(&state); err != nil {
		return err
	}
	if state == "disabled" {
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'offline',
			routing_ready = 0, updated_at = ? WHERE id = ?`, now, nodeID); err != nil {
			return err
		}
	}
	if err := seedNodeTargets(ctx, tx, nodeID, now); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, "node.enable", "node", nodeID, "success",
		requestID, "节点已启用并等待心跳", admin); err != nil {
		return err
	}
	return tx.Commit()
}

func (r Repository) DeleteNode(ctx context.Context, nodeID, requestID, admin string) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, nodeID).Scan(&exists); err != nil {
		return err
	}
	if err := deleteNodeData(ctx, tx, nodeID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, "node.delete", "node", nodeID, "success",
		requestID, "节点已删除", admin); err != nil {
		return err
	}
	r.runtime().CloseNodeSessions(nodeID)
	return tx.Commit()
}

func (r Repository) SyncReset(ctx context.Context, nodeID, requestID, admin string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var state string
	if err := r.DB.QueryRowContext(ctx, `SELECT state FROM nodes WHERE id = ?`, nodeID).Scan(&state); err != nil {
		return err
	}
	if state != "disabled" {
		_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET routing_ready = 0,
		updated_at = ? WHERE id = ? AND state != 'disabled'`, now, nodeID)
		if err != nil {
			return err
		}
	}
	return r.Audit(ctx, "node.sync_reset", "node", nodeID, "success", requestID, "同步状态已重置", admin)
}

func (r Repository) Audit(ctx context.Context, op, targetType, targetID, result, requestID, summary, admin string) error {
	return auditTx(ctx, r.DB, op, targetType, targetID, result, requestID, summary, admin)
}

func auditTx(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, op, targetType, targetID, result, requestID, summary, admin string) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO admin_audit_events
		(id, operation, target_type, target_id, result, request_id, created_at,
		admin_identity, details_summary) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mustID(), op, targetType, targetID, result, requestID,
		time.Now().UTC().Format(time.RFC3339Nano), admin, summary)
	return err
}

func (r Repository) LatestHeartbeat(ctx context.Context, nodeID string) (map[string]any, error) {
	return r.runtime().LatestHeartbeat(nodeID)
}

func (r Repository) LatestInventoryReport(ctx context.Context, nodeID string) (map[string]any, error) {
	return r.runtime().LatestInventoryReport(nodeID)
}

func (r Repository) LatestPressureReport(ctx context.Context, nodeID string) (map[string]any, error) {
	return r.runtime().LatestPressureReport(nodeID)
}
