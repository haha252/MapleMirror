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
	if !report.Complete {
		return false, nil
	}
	id := report.ReportID
	if id == "" {
		var err error
		id, err = requestid.New()
		if err != nil {
			return false, err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO node_inventory_reports
		(id, node_id, revision, complete, item_count, result, request_id, reported_at)
		VALUES (?, ?, ?, 1, ?, 'accepted', ?, ?)`,
		id, session.NodeID, report.Revision, len(report.Items), session.RequestID, now)
	return false, err
}

func (r Repository) acceptStaleInventoryReport(ctx context.Context, tx *sql.Tx, session Session,
	seq uint64, report protocol.InventoryReport, now string) (HeartbeatResult, error) {
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
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
