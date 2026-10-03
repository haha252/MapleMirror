package control

import (
	"context"
	"time"

	"mirror-server/internal/controlv2"
)

func (s *V2Server) v2TaskWakeLoop(ctx context.Context, session Session, queue *controlv2.Queue) {
	s.v2TaskWakeLoopEvery(ctx, session, queue, 30*time.Second)
}

func (s *V2Server) v2TaskWakeLoopEvery(ctx context.Context, session Session, queue *controlv2.Queue, interval time.Duration) {
	wake := s.Repo.runtime().SyncTaskWakeChannel(session.NodeID)
	probeTicker := time.NewTicker(interval)
	defer probeTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			_ = s.Repo.runtime().ConsumeSyncTaskWake(session.NodeID)
			_ = s.dispatchV2Cancellations(ctx, session, queue)
			_ = s.dispatchV2Tasks(ctx, session, queue)
			_ = s.dispatchV2Authorizations(ctx, session, queue)
		case <-probeTicker.C:
			// Retry timers are process-local and disappear on master restart.
			// Also reclaim expired leases when slot counts haven't changed.
			_ = s.dispatchV2Cancellations(ctx, session, queue)
			_ = s.dispatchV2Tasks(ctx, session, queue)
			_ = s.maybeDispatchV2PublicProbe(session, queue)
		}
	}
}
