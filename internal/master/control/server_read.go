package control

import (
	"context"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const controlWakeReadPoll = 200 * time.Millisecond

func (s ControlServer) readControlFrameOrDispatchWake(conn net.Conn,
	session Session, reqID string, readers ...*protocol.FrameReader) (protocol.Envelope, int, error) {
	deadline := time.Now().Add(s.sessionReadTimeout())
	for {
		if s.Repo.runtime().ConsumeSyncTaskWake(session.NodeID) {
			auths, err := s.dispatchDownloadAuthorizations(conn, session, reqID, readers...)
			if err != nil {
				return protocol.Envelope{}, auths, err
			}
			dispatched, _, err := s.dispatchSyncTasksInteractively(conn, session, reqID,
				controlMessageResult{DispatchSyncTasks: true}, readers...)
			if err != nil {
				return protocol.Envelope{}, auths + dispatched, err
			}
			if auths+dispatched > 0 {
				return protocol.Envelope{}, auths + dispatched, nil
			}
			if ready, err := s.Repo.hasDispatchableSyncTask(context.Background(), session.NodeID); err != nil {
				return protocol.Envelope{}, auths + dispatched, err
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
		msg, err := readControlFrame(conn, timeout, readers...)
		if err == nil {
			return msg, 0, nil
		}
		if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() && time.Now().Before(deadline) {
			continue
		}
		return protocol.Envelope{}, 0, err
	}
}
