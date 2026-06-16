package control

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"mirror-server/internal/master/assignment"
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
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	now := r.runtime().InventoryBatchTime(session.NodeID, report.Revision, time.Now().UTC())
	stale, err := acceptInventoryRevision(ctx, tx, session, report, now)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if stale {
		return r.acceptStaleInventoryReport(ctx, tx, session, seq, report, now)
	}
	reported := make(map[string]bool, len(report.Items))
	quarantined := false
	for _, item := range report.Items {
		reported[item.AssetID] = true
		result, err := acceptInventoryItem(ctx, tx, session.NodeID, item, now)
		if err != nil {
			return HeartbeatResult{}, err
		}
		if result.PublicAsset && result.State == "mismatch" {
			if err := r.quarantineNodeForPublicAssetMismatch(ctx, tx, session, result, now); err != nil {
				return HeartbeatResult{}, err
			}
			quarantined = true
			break
		}
		if result.State == "verified" {
			if err := publishVerifiedAsset(ctx, tx, result.AssetID, now); err != nil {
				return HeartbeatResult{}, err
			}
		}
	}
	if report.Complete && !quarantined {
		if err := markMissingInventory(ctx, tx, session.NodeID, now, reported); err != nil {
			return HeartbeatResult{}, err
		}
		clearedTasks, err := clearSatisfiedDownloadTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return HeartbeatResult{}, err
		}
		generatedTasks, err := createRepairTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return HeartbeatResult{}, err
		}
		completedReconcileTasks, err := completeInventoryReconcileTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return HeartbeatResult{}, err
		}
		if _, err := r.reconcileNodeReady(ctx, tx, session.NodeID, now); err != nil {
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
				slog.Int("generated_repair_tasks", generatedTasks),
				slog.Int("cleared_satisfied_tasks", clearedTasks),
				slog.Int("completed_reconcile_tasks", completedReconcileTasks),
				slog.Bool("routing_ready", ready))
		}
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	if report.Complete || quarantined {
		r.runtime().FinishInventoryBatch(session.NodeID, report.Revision)
	}
	r.runtime().MarkInventory(session.NodeID, runtimeInventoryReport{
		Revision: int(report.Revision), Complete: report.Complete,
		ItemCount: len(report.Items), Result: "accepted",
		RequestID: session.RequestID, Reported: now, Valid: true,
	})
	if quarantined {
		r.runtime().CloseNodeSessions(session.NodeID)
		return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(false), RoutingReady: false}, nil
	}
	ready := r.nodeRoutingReady(ctx, session.NodeID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, nil
}

func acceptInventoryItem(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, nodeID string, item protocol.InventoryItem, now string) (inventoryAcceptResult, error) {
	result := inventoryAcceptResult{
		AssetID:     item.AssetID,
		LocalDigest: item.DigestSHA256,
		LocalSize:   item.SizeBytes,
	}
	var expectedDigest string
	var expectedSize int64
	err := tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, item.AssetID).Scan(&expectedDigest, &expectedSize)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !inventoryTargetRequired(ctx, tx, nodeID, item.AssetID) {
		return result, nil
	}
	result.ExpectedDigest = expectedDigest
	result.ExpectedSize = expectedSize
	result.PublicAsset = publicCandidateAsset(ctx, tx, item.AssetID)
	localDigest := item.DigestSHA256
	localSize := item.SizeBytes
	previousState, previousDigest, previousSize, err := currentInventory(ctx, tx, nodeID, item.AssetID)
	if err != nil {
		return result, err
	}
	state := "verified"
	switch item.LocalState {
	case "missing":
		state = "missing"
		if localDigest == "" {
			localSize = 0
		}
	case "mismatch":
		state = "mismatch"
	default:
		if previousState == "stale" && localDigest == previousDigest && localSize == previousSize {
			state = "stale"
		} else if localDigest != expectedDigest || localSize != expectedSize {
			state = "mismatch"
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
		state = excluded.state`,
		nodeID, item.AssetID, localDigest, localSize, now, state)
	result.State = state
	result.LocalDigest = localDigest
	result.LocalSize = localSize
	return result, err
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
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'online',
		last_heartbeat_at = ?,
		target_bandwidth_bps = CASE WHEN ? > 0 THEN ? ELSE target_bandwidth_bps END,
		max_mirror_projects = ?,
		updated_at = ? WHERE id = ?`,
		now, report.TargetBandwidthBPS, report.TargetBandwidthBPS,
		nonNegative(report.MaxMirrorProjects), now, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if previousMax != nonNegative(report.MaxMirrorProjects) {
		if err := assignment.ReconcileNode(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
		if _, err := assignment.GenerateNodeTasks(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
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
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, nil
}
