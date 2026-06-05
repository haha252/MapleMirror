package control

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) AcceptSyncTaskAck(ctx context.Context, session Session, seq uint64, ack protocol.SyncTaskAck) (HeartbeatResult, error) {
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
	state := ack.State
	if state == "" || state == "accepted" {
		state = "running"
	}
	if state != "running" && state != "failed" {
		state = "running"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE node_tasks SET state = ?,
		error_message = ?, updated_at = ? WHERE id = ? AND node_id = ?
		AND state IN ('sent', 'pending', 'retry_wait')`,
		state, nullable(ack.Message), now, ack.TaskID, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	ready := routingReady(ctx, tx, session.NodeID)
	if r.Logger != nil {
		r.Logger.Debug(ctx, "同步任务确认已处理",
			slog.String("node_id", session.NodeID),
			slog.String("task_id", ack.TaskID),
			slog.String("state", state),
			slog.String("message", ack.Message))
	}
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
}
