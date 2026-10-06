package control

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
	"strings"
	"time"
)

func (s *V2Server) dispatchV2Tasks(ctx context.Context, session Session, queue *controlv2.Queue) error {
	unlock := s.Repo.runtime().lockV2Tasks(session.NodeID)
	defer unlock()
	if _, err := s.Repo.runtime().CurrentSequence(session); err != nil {
		return err
	}
	if _, known := s.Repo.runtime().LatestV2Status(session.NodeID); !known {
		return nil // A replacement connection must report its own capacity first.
	}
	if err := s.pruneV2TaskReplay(ctx, session.NodeID, queue); err != nil {
		s.logV2TaskCapacityError(ctx, session.NodeID, err)
		return err
	}
	for i := 0; i < controlv2.ReplayWindow; i++ {
		keys := queue.ReplayKeys(protocolv2.TypeSyncTask)
		if len(keys) >= controlv2.ReplayWindow {
			return nil
		}
		excluded := make([]string, 0, len(keys))
		for _, key := range keys {
			excluded = append(excluded, strings.SplitN(key, "\x00", 2)[0])
		}
		allowance, err := s.Repo.v2DispatchAllowance(ctx, session.NodeID)
		if err != nil {
			s.logV2TaskCapacityError(ctx, session.NodeID, err)
			return err
		}
		task, ok, err := s.Repo.nextV2SyncTaskForDispatch(ctx, session.NodeID, excluded, allowance > 0)
		if err != nil || !ok {
			return err
		}
		envelope, err := protocolv2.New(protocolv2.TypeSyncTask, protocolv2.StableMessageID(protocolv2.TypeSyncTask, task.TaskID, task.AttemptID), task)
		if err != nil {
			return err
		}
		if err := queue.EnqueueReplay(envelope, task.TaskID+"\x00"+task.AttemptID); err != nil {
			if errors.Is(err, controlv2.ErrQueueFull) || errors.Is(err, controlv2.ErrReplayWindowFull) {
				return nil // The persisted sent attempt will be reloaded later.
			}
			return err
		}
	}
	return nil
}

func (s *V2Server) logV2TaskCapacityError(ctx context.Context, nodeID string, err error) {
	logger := s.Logger
	if logger == nil {
		logger = s.Repo.Logger
	}
	if logger != nil && ctx.Err() == nil {
		logger.Warn(ctx, "同步任务容量查询失败，暂停下发", slog.String("node_id", nodeID), slog.String("error", err.Error()))
	}
}

func (s *V2Server) pruneV2TaskReplay(ctx context.Context, nodeID string, queue *controlv2.Queue) error {
	for _, key := range queue.ReplayKeys(protocolv2.TypeSyncTask) {
		identity := strings.SplitN(key, "\x00", 2)
		if len(identity) != 2 {
			continue
		}
		var state, attempt, lease string
		err := s.Repo.DB.QueryRowContext(ctx, `SELECT state, COALESCE(attempt_id,''), COALESCE(lease_expires_at,'') FROM node_tasks WHERE id=? AND node_id=?`, identity[0], nodeID).Scan(&state, &attempt, &lease)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows || state != "sent" || attempt != identity[1] || lease <= time.Now().UTC().Format(time.RFC3339Nano) {
			queue.Acknowledge(protocolv2.StableMessageID(protocolv2.TypeSyncTask, identity[0], identity[1]))
		}
	}
	return nil
}
