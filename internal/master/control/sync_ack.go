package control

import (
	"context"
	"fmt"
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
	if state == "failed" {
		return HeartbeatResult{}, fmt.Errorf("同步任务 ACK 无效：失败状态必须通过任务结果上报")
	}
	if state != "running" {
		state = "running"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = ?,
		error_message = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = ? AND node_id = ?
		AND state IN ('sent', 'running')`,
		state, nullable(ack.Message),
		time.Now().UTC().Add(syncTaskLeaseDuration).Format(time.RFC3339Nano),
		now, ack.TaskID, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return HeartbeatResult{}, fmt.Errorf("同步任务 ACK 无效：任务不存在或状态不允许确认")
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
