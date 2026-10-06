package control

import (
	"context"
	"log/slog"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) rejectV2Task(ctx context.Context, session Session, rejected protocolv2.SyncRejected) error {
	unlock := r.runtime().lockV2Tasks(session.NodeID)
	defer unlock()
	if _, err := r.runtime().CurrentSequence(session); err != nil {
		return err
	}
	now := time.Now().UTC()
	retryAt := now.Add(5 * time.Second)
	backpressure := v2RejectionIsBackpressure(rejected)
	increment := 1
	if backpressure {
		increment = 0
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE node_tasks SET state='retry_wait', attempts=COALESCE(attempts,0)+?,
		error_message=?, retry_after=?, lease_expires_at=NULL, updated_at=?
		WHERE id=? AND node_id=? AND attempt_id=? AND state='sent'`,
		increment, rejected.Reason, retryAt.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), rejected.TaskID, session.NodeID, rejected.AttemptID)
	if err == nil {
		if n, _ := result.RowsAffected(); n > 0 {
			if backpressure {
				state := r.runtime().v2DispatchState(session.NodeID)
				state.PauseUntil = retryAt
				if rejected.CapacityRevision > state.CapacityBarrier {
					state.CapacityBarrier = rejected.CapacityRevision
				}
				// A newer status may already have overtaken this rejection.
				latest, known := r.runtime().LatestV2Status(session.NodeID)
				state.WaitingStatus = !known || rejected.CapacityRevision == 0 || latest.Status.CapacityRevision <= state.CapacityBarrier
				r.runtime().setV2DispatchState(session.NodeID, state)
				if r.Logger != nil {
					r.Logger.Debug(ctx, "节点暂缓接收同步任务", slog.String("node_id", session.NodeID),
						slog.String("task_id", rejected.TaskID), slog.String("attempt_id", rejected.AttemptID),
						slog.String("reason", rejected.Reason), slog.String("retry_after", retryAt.Format(time.RFC3339Nano)))
				}
			}
			r.scheduleSyncTaskRetryWakeAt(session.NodeID, retryAt)
		}
	}
	return err
}

func v2RejectionIsBackpressure(rejected protocolv2.SyncRejected) bool {
	switch rejected.Code {
	case protocolv2.SyncRejectedSlotsFull, protocolv2.SyncRejectedPreviousAttemptStopping:
		return true
	case "": // Existing nodes report only the original English reason.
		return rejected.Reason == "sync task slots full" || rejected.Reason == "previous attempt is still stopping"
	default:
		return false
	}
}
