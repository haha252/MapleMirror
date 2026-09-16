package control

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

type frozenV2Task struct {
	TaskID    string
	NodeID    string
	AttemptID string
}

func (s *V2Server) freezeV2SwarmAsset(ctx context.Context, assetID string) error {
	tx, err := s.Repo.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,node_id,COALESCE(attempt_id,'') FROM node_tasks
		WHERE asset_id=? AND state IN ('sent','running') AND COALESCE(attempt_id,'')!=''`, assetID)
	if err != nil {
		return err
	}
	var active []frozenV2Task
	for rows.Next() {
		var item frozenV2Task
		if err := rows.Scan(&item.TaskID, &item.NodeID, &item.AttemptID); err != nil {
			rows.Close()
			return err
		}
		active = append(active, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state='pending',attempt_id='',lease_expires_at=NULL,
		retry_after=NULL,error_message='swarm manifest conflict; whole-file retry',updated_at=?
		WHERE asset_id=? AND state IN ('sent','running')`, now, assetID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.Repo.runtime().ClearSwarmAsset(assetID)
	nodes := make([]string, 0, len(active))
	seen := make(map[string]struct{}, len(active))
	for _, item := range active {
		if qv, ok := s.queues.Load(item.NodeID); ok {
			if queue, ok := qv.(*controlv2.Queue); ok {
				envelope, _ := protocolv2.New(protocolv2.TypeSyncCancel, mustID(), protocolv2.SyncCancel{
					TaskID: item.TaskID, AttemptID: item.AttemptID, Reason: "swarm manifest conflict; retry in whole-file mode",
				})
				_ = queue.Enqueue(envelope, item.TaskID+"/"+item.AttemptID)
			}
		}
		if _, ok := seen[item.NodeID]; !ok {
			seen[item.NodeID] = struct{}{}
			nodes = append(nodes, item.NodeID)
		}
	}
	if len(nodes) > 0 {
		s.Repo.runtime().NotifySyncTasks(nodes...)
	}
	if s.Logger != nil {
		s.Logger.Error(ctx, "Swarm Manifest 冲突，已冻结该资产的 partial Swarm",
			slog.String("asset_id", assetID), slog.Int("cancelled_attempts", len(active)))
	}
	return nil
}
