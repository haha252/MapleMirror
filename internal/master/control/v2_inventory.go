package control

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"mirror-server/internal/master/assignment"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) AcceptV2InventorySegment(ctx context.Context, session Session, segment protocolv2.InventorySnapshotSegment, reportID string) (bool, error) {
	if segment.Revision == 0 || len(segment.Items) > 1000 {
		return false, fmt.Errorf("invalid inventory segment")
	}
	report := protocol.InventoryReport{ReportID: reportID, Revision: segment.Revision, GeneratedAt: segment.GeneratedAt, Complete: segment.Complete}
	report.Items = make([]protocol.InventoryItem, 0, len(segment.Items))
	for _, item := range segment.Items {
		report.Items = append(report.Items, protocol.InventoryItem{AssetID: item.AssetID, SizeBytes: item.SizeBytes, DigestSHA256: item.DigestSHA256, LocalState: item.LocalState})
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	receivedAt := inventoryReceiptTime()
	duplicate, err := acceptV2InventorySegmentReceipt(ctx, tx, session.NodeID, segment, reportID, receivedAt)
	if err != nil {
		return false, err
	}
	if duplicate {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return segment.Complete, nil
	}
	now, err := inventoryReportTime(ctx, tx, r.runtime(), session.NodeID, report.Revision, time.Now().UTC())
	if err != nil {
		return false, err
	}
	stale, err := acceptInventoryRevision(ctx, tx, session, report, now)
	if err != nil {
		return false, err
	}
	if stale {
		if report.Complete {
			_, err = r.reconcileNodeReady(ctx, tx, session.NodeID, now)
			if err != nil {
				return false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		if report.Complete {
			r.runtime().FinishInventoryBatch(session.NodeID, report.Revision)
		}
		return report.Complete, nil
	}
	verifiedProjects, quarantined, err := r.acceptInventoryItems(ctx, tx, session, report.Items, now)
	if err != nil {
		return false, err
	}
	var pendingNodes []string

	nodes, err := publishVerifiedProjects(ctx, tx, verifiedProjects, now)
	if err != nil {
		return false, err
	}
	pendingNodes = append(pendingNodes, nodes...)
	changed := len(nodes) > 0
	if report.Complete && !quarantined {
		marked, err := markNonRequiredVerifiedInventoryRemoved(ctx, tx, session.NodeID, now)
		if err != nil {
			return false, err
		}
		if err := markMissingInventory(ctx, tx, session.NodeID, now, nil); err != nil {
			return false, err
		}
		if err := setCompleteInventoryItemCount(ctx, tx, session.NodeID, report.Revision, now); err != nil {
			return false, err
		}
		reset, err := resetMissingDownloadTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return false, err
		}
		cleared, err := clearSatisfiedDownloadTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return false, err
		}
		generated, err := createRepairTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return false, err
		}
		deleted, err := assignment.GenerateNodeDeleteTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return false, err
		}
		changed = changed || reset > 0 || generated > 0 || deleted > 0
		completed, err := completeInventoryReconcileTasks(ctx, tx, session.NodeID, now)
		if err != nil {
			return false, err
		}
		if _, err := r.reconcileNodeReady(ctx, tx, session.NodeID, now); err != nil {
			return false, err
		}
		if r.Logger != nil {
			r.Logger.Debug(ctx, "control.v2 完整库存已接收", slog.String("node_id", session.NodeID), slog.Uint64("revision", report.Revision), slog.Int("marked_remove", marked), slog.Int("reset", reset), slog.Int("generated", generated), slog.Int("deleted", deleted), slog.Int("cleared", cleared), slog.Int("completed_reconcile", completed))
		}
	}
	if report.Complete {
		if err := pruneV2InventorySegmentReceipts(ctx, tx, session.NodeID, report.Revision); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if changed {
		r.runtime().NotifySyncTasks(session.NodeID)
	}
	if len(pendingNodes) > 0 {
		r.runtime().NotifySyncTasks(pendingNodes...)
	}
	if report.Complete || quarantined {
		r.runtime().FinishInventoryBatch(session.NodeID, report.Revision)
	}
	r.runtime().MarkInventory(session.NodeID, runtimeInventoryReport{Revision: int(report.Revision), Complete: report.Complete, ItemCount: len(report.Items), Result: "accepted", RequestID: session.RequestID, Reported: now, Valid: true})
	return report.Complete && !quarantined, nil
}
