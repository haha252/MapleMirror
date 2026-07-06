package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

func acceptInventoryRevision(ctx context.Context, tx *sql.Tx, session Session,
	report protocol.InventoryReport, now string) (bool, error) {
	var latest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(revision)
		FROM node_inventory_reports WHERE node_id = ? AND complete = 1`,
		session.NodeID).Scan(&latest); err != nil {
		return false, err
	}
	if latest.Valid && uint64(latest.Int64) >= report.Revision {
		return true, nil
	}
	// 库存报告只承担当前 revision 的恢复边界，不在在线数据库保留历史。
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_inventory_reports
		WHERE node_id = ? AND revision != ?`, session.NodeID, report.Revision); err != nil {
		return false, err
	}
	id := report.ReportID
	if id == "" {
		var err error
		id, err = requestid.New()
		if err != nil {
			return false, err
		}
	}
	if !report.Complete {
		_, err := tx.ExecContext(ctx, `INSERT INTO node_inventory_reports
			(id, node_id, revision, complete, item_count, result, request_id, reported_at)
			VALUES (?, ?, ?, 0, ?, 'partial', ?, ?)
			ON CONFLICT(node_id, revision) DO UPDATE SET
			item_count = node_inventory_reports.item_count + excluded.item_count,
			result = 'partial'`,
			id, session.NodeID, report.Revision, len(report.Items), session.RequestID, now)
		return false, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO node_inventory_reports
		(id, node_id, revision, complete, item_count, result, request_id, reported_at)
		VALUES (?, ?, ?, 1, ?, 'accepted', ?, ?)
		ON CONFLICT(node_id, revision) DO UPDATE SET
		complete = 1,
		item_count = node_inventory_reports.item_count + excluded.item_count,
		result = 'accepted',
		request_id = excluded.request_id`,
		id, session.NodeID, report.Revision, len(report.Items), session.RequestID, now)
	return false, err
}

func (r Repository) acceptStaleInventoryReport(ctx context.Context, tx *sql.Tx, session Session,
	seq uint64, report protocol.InventoryReport, now string) (HeartbeatResult, error) {
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	if report.Complete {
		if _, err := r.reconcileNodeReady(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	r.runtime().FinishInventoryBatch(session.NodeID, report.Revision)
	r.runtime().MarkInventory(session.NodeID, runtimeInventoryReport{
		Revision: int(report.Revision), Complete: report.Complete,
		ItemCount: len(report.Items), Result: "stale",
		RequestID: session.RequestID, Reported: now, Valid: true,
	})
	ready := r.nodeRoutingReady(ctx, session.NodeID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, nil
}

func setCompleteInventoryItemCount(ctx context.Context, tx *sql.Tx, nodeID string,
	revision uint64, reportedAt string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_inventory
		WHERE node_id = ? AND verified_at = ?`, nodeID, reportedAt).Scan(&count); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE node_inventory_reports
		SET item_count = ? WHERE node_id = ? AND revision = ?`,
		count, nodeID, revision)
	return err
}
