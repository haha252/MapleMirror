package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

// Compare process-reported attempts with durable desired state. This also
// replays cancellation after queue failure, reconnect, or master restart.
func (s *V2Server) dispatchV2Cancellations(ctx context.Context, session Session, queue *controlv2.Queue) error {
	latest, ok := s.Repo.runtime().LatestV2Status(session.NodeID)
	if !ok {
		return nil
	}
	for _, active := range latest.Status.ActiveTasks {
		var attempt, state string
		err := s.Repo.DB.QueryRowContext(ctx, `SELECT COALESCE(attempt_id,''),state FROM node_tasks WHERE id=? AND node_id=?`, active.TaskID, session.NodeID).Scan(&attempt, &state)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && attempt == active.AttemptID && (state == "sent" || state == "running") {
			continue
		}
		body := protocolv2.SyncCancel{TaskID: active.TaskID, AttemptID: active.AttemptID, Reason: "任务已取消或同步流程已重置"}
		envelope, err := protocolv2.New(protocolv2.TypeSyncCancel, mustID(), body)
		if err != nil {
			return err
		}
		if err := queue.Enqueue(envelope, active.TaskID+"/"+active.AttemptID); err != nil {
			return err
		}
	}
	return nil
}
