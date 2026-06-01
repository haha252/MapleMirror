package control

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
		VALUES (?, ?, ?, ?, ?, 'accepted', ?, ?)
		ON CONFLICT(node_id, revision) DO UPDATE SET
		complete = excluded.complete,
		item_count = excluded.item_count,
		result = excluded.result,
		request_id = excluded.request_id,
		reported_at = excluded.reported_at`,
		mustID(), session.NodeID, report.Revision, boolInt(report.Complete),
		len(report.Items), session.RequestID, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	for _, item := range report.Items {
		if err := acceptInventoryItem(ctx, tx, session.NodeID, item, now); err != nil {
			return HeartbeatResult{}, err
		}
	}
	if report.Complete {
		if err := r.reconcileNodeReady(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
		if r.Logger != nil {
			missing, running, ready := readySnapshot(ctx, tx, session.NodeID)
			r.Logger.Debug(ctx, "节点完整库存上报已接收",
				slog.String("node_id", session.NodeID),
				slog.Uint64("revision", report.Revision),
				slog.Int("item_count", len(report.Items)),
				slog.Bool("complete", report.Complete),
				slog.Int("missing_targets", missing),
				slog.Int("running_tasks", running),
				slog.Bool("routing_ready", ready))
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET
		last_message_sequence = ? WHERE id = ?`, seq, session.ID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, finish(tx, err)
}

func acceptInventoryItem(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, nodeID string, item protocol.InventoryItem, now string) error {
	var expectedDigest string
	var expectedSize int64
	err := tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, item.AssetID).Scan(&expectedDigest, &expectedSize)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	state := "verified"
	if item.DigestSHA256 != expectedDigest || item.SizeBytes != expectedSize {
		state = "mismatch"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
		state = excluded.state`,
		nodeID, item.AssetID, item.DigestSHA256, item.SizeBytes, now, state)
	return err
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
		last_heartbeat_at = ?, updated_at = ? WHERE id = ?`,
		now, now, session.NodeID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, finish(tx, err)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
