package control

import (
	"context"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) AcceptInventoryReport(ctx context.Context, session Session, seq uint64, report protocol.InventoryReport) (HeartbeatResult, error) {
	if len(report.Items) > 1000 {
		return HeartbeatResult{}, fmt.Errorf("库存报告条目过多")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	var last uint64
	if err := tx.QueryRowContext(ctx, `SELECT last_message_sequence FROM node_control_sessions
		WHERE id = ? AND disconnected_at IS NULL`, session.ID).Scan(&last); err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		return HeartbeatResult{AcceptedSequence: last, ManagedState: "syncing"}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory_reports
		(id, node_id, revision, complete, item_count, result, request_id, reported_at)
		VALUES (?, ?, ?, ?, ?, 'accepted', ?, ?)`,
		mustID(), session.NodeID, report.Revision, boolInt(report.Complete),
		len(report.Items), session.RequestID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET
		last_message_sequence = ? WHERE id = ?`, seq, session.ID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, finish(tx, err)
}

func (r Repository) AcceptPressureReport(ctx context.Context, session Session, seq uint64, report protocol.PressureReport) (HeartbeatResult, error) {
	if report.SampleWindowSeconds <= 0 || report.ActiveDownloads < 0 || report.FreeBytes < 0 {
		return HeartbeatResult{}, fmt.Errorf("压力报告数值不合法")
	}
	ratio := float64(0)
	if report.TargetBandwidthBPS > 0 {
		ratio = float64(report.ActualBandwidthBPS) / float64(report.TargetBandwidthBPS)
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	var last uint64
	if err := tx.QueryRowContext(ctx, `SELECT last_message_sequence FROM node_control_sessions
		WHERE id = ? AND disconnected_at IS NULL`, session.ID).Scan(&last); err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		return HeartbeatResult{AcceptedSequence: last, ManagedState: "syncing"}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO node_pressure_reports
		(id, node_id, pressure_ratio, active_downloads, free_bytes, request_id, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		mustID(), session.NodeID, ratio, report.ActiveDownloads,
		report.FreeBytes, session.RequestID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_heartbeats
		(id, node_id, state, pressure_ratio, active_downloads, free_bytes, reported_at)
		VALUES (?, ?, 'syncing', ?, ?, ?, ?)`,
		mustID(), session.NodeID, ratio, report.ActiveDownloads, report.FreeBytes, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET
		last_message_sequence = ?, last_heartbeat_at = ? WHERE id = ?`, seq, now, session.ID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'syncing',
		last_heartbeat_at = ?, routing_ready = 0, updated_at = ? WHERE id = ?`,
		now, now, session.NodeID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, finish(tx, err)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
