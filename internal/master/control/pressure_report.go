package control

import (
	"context"
	"fmt"
	"time"

	"mirror-server/internal/master/assignment"
	"mirror-server/internal/protocol"
)

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
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	var previousMax int
	if err := tx.QueryRowContext(ctx, `SELECT max_mirror_projects FROM nodes
		WHERE id = ?`, session.NodeID).Scan(&previousMax); err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET
		state = CASE WHEN state = 'disabled' THEN state ELSE 'online' END,
		last_heartbeat_at = ?,
		target_bandwidth_bps = CASE WHEN ? > 0 THEN ? ELSE target_bandwidth_bps END,
		max_mirror_projects = ?,
		updated_at = ? WHERE id = ?`,
		now, report.TargetBandwidthBPS, report.TargetBandwidthBPS,
		nonNegative(report.MaxMirrorProjects), now, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	syncTasksChanged := false
	if previousMax != nonNegative(report.MaxMirrorProjects) {
		if err := assignment.ReconcileNode(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
		generated, err := assignment.GenerateNodeTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return HeartbeatResult{}, err
		}
		syncTasksChanged = generated > 0
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	r.runtime().MarkPressure(session.NodeID, runtimePressureReport{
		PressureRatio: ratio, ActiveDownloads: int64(report.ActiveDownloads),
		FreeBytes: report.FreeBytes, TargetBandwidth: report.TargetBandwidthBPS,
		ActualBandwidth: report.ActualBandwidthBPS,
		RequestID:       session.RequestID, ReportedAt: now, Valid: true,
	})
	r.runtime().MarkHeartbeat(session.NodeID, runtimeHeartbeat{
		State: "syncing", PressureRatio: ratio, ActiveDownloads: int64(report.ActiveDownloads),
		FreeBytes: report.FreeBytes, TargetBandwidth: report.TargetBandwidthBPS,
		ActualBandwidth: report.ActualBandwidthBPS, ReportedAt: now, Valid: true,
	})
	ready := r.nodeRoutingReady(ctx, session.NodeID)
	return HeartbeatResult{
		AcceptedSequence: seq, ManagedState: managedState(ready),
		RoutingReady: ready, SyncTasksChanged: syncTasksChanged,
	}, nil
}
