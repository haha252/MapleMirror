package control

import (
	"context"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const controlWakeReadPoll = 200 * time.Millisecond

func (s ControlServer) readControlFrameOrDispatchWake(conn net.Conn,
	session Session, reqID string) (protocol.Envelope, int, error) {
	deadline := time.Now().Add(s.sessionReadTimeout())
	for {
		if s.Repo.runtime().ConsumeSyncTaskWake(session.NodeID) {
			dispatched, _, err := s.dispatchSyncTasksInteractively(conn, session, reqID,
				controlMessageResult{DispatchSyncTasks: true})
			if err != nil {
				return protocol.Envelope{}, dispatched, err
			}
			if dispatched > 0 {
				return protocol.Envelope{}, dispatched, nil
			}
			if ready, err := s.Repo.hasDispatchableSyncTask(context.Background(), session.NodeID); err != nil {
				return protocol.Envelope{}, dispatched, err
			} else if ready {
				s.Repo.runtime().NotifySyncTasks(session.NodeID)
			}
		}
		timeout := time.Until(deadline)
		if timeout <= 0 {
			timeout = time.Nanosecond
		}
		if timeout > controlWakeReadPoll {
			timeout = controlWakeReadPoll
		}
		msg, err := readControlFrame(conn, timeout)
		if err == nil {
			return msg, 0, nil
		}
		if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() && time.Now().Before(deadline) {
			continue
		}
		return protocol.Envelope{}, 0, err
	}
}
