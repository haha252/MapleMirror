package control

import (
	"context"
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
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	now, err := inventoryReportTime(ctx, tx, r.runtime(), session.NodeID, report.Revision, time.Now().UTC())
	if err != nil {
		return HeartbeatResult{}, err
	}
	syncTasksChanged := false
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
		resetTasks, err := resetMissingDownloadTasks(ctx, tx, session.NodeID, now)
		if err != nil {
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
		syncTasksChanged = resetTasks > 0 || generatedTasks > 0
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
				slog.Int("reset_missing_tasks", resetTasks),
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
	if syncTasksChanged {
		r.runtime().NotifySyncTasks(session.NodeID)
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
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready, SyncTasksChanged: syncTasksChanged}, nil
}
