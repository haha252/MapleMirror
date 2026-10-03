package control

import (
	"context"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) rejectV2Task(ctx context.Context, session Session, rejected protocolv2.SyncRejected) error {
	retryAt := time.Now().UTC().Add(5 * time.Second)
	result, err := r.DB.ExecContext(ctx, `UPDATE node_tasks SET state='retry_wait', attempts=COALESCE(attempts,0)+1,
		error_message=?, retry_after=?, lease_expires_at=NULL, updated_at=?
		WHERE id=? AND node_id=? AND attempt_id=? AND state IN ('sent','running')`,
		rejected.Reason, retryAt.Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano), rejected.TaskID, session.NodeID, rejected.AttemptID)
	if err == nil {
		if n, _ := result.RowsAffected(); n > 0 {
			r.scheduleSyncTaskRetryWakeAt(session.NodeID, retryAt)
		}
	}
	return err
}
