package control

import (
	"context"
	"fmt"
	"net"

	"mirror-server/internal/protocol"
)

func (s ControlServer) dispatchSyncTasksInteractively(conn net.Conn, session Session,
	reqID string, result controlMessageResult) (int, controlMessageResult, error) {
	if !result.DispatchSyncTasks {
		return 0, result, nil
	}
	if err := s.Repo.refreshExpiredSyncTaskLeases(context.Background(), session.NodeID); err != nil {
		return 0, result, err
	}
	outstanding, err := s.Repo.outstandingSentSyncTasks(context.Background(), session.NodeID)
	if err != nil {
		return 0, result, err
	}
	dispatched := 0
	for {
		if s.syncTaskDispatchAllowance(session.NodeID, outstanding) <= 0 {
			return dispatched, result, nil
		}
		ok, err := s.writeNextTask(conn, session, reqID)
		if err != nil || !ok {
			return dispatched, result, err
		}
		dispatched++
		outstanding++
		for {
			ack, ackResult, err := s.readInterleavedControlMessage(conn, session, reqID)
			if err != nil {
				return dispatched, result, err
			}
			if err := s.writeMessageAck(conn, session, reqID, ack, ackResult.HeartbeatResult); err != nil {
				return dispatched, result, err
			}
			result = ackResult
			if isSyncTaskResponse(ack.MessageType) {
				if outstanding > 0 {
					outstanding--
				}
				break
			}
		}
	}
}

func isSyncTaskResponse(messageType string) bool {
	return messageType == protocol.TypeSyncTaskAck || messageType == protocol.TypeSyncTaskResult
}

func (s ControlServer) readInterleavedControlMessage(conn net.Conn, session Session,
	reqID string) (protocol.Envelope, controlMessageResult, error) {
	msg, err := readControlFrame(conn, s.sessionReadTimeout())
	if err != nil {
		return protocol.Envelope{}, controlMessageResult{}, err
	}
	if msg.NodeID != session.NodeID {
		return msg, controlMessageResult{}, fmt.Errorf("节点标识不匹配")
	}
	if err := msg.Validate(protocol.Control); err != nil {
		return msg, controlMessageResult{}, err
	}
	result, err := s.handleMessage(session, msg)
	return msg, result, err
}

func (s ControlServer) writeResponsesAfterMessage(conn net.Conn, session Session, reqID string,
	msg protocol.Envelope, result controlMessageResult) (int, error) {
	dispatched := 0
	auths, err := s.dispatchDownloadAuthorizations(conn, session, reqID)
	if err != nil {
		return auths, err
	}
	dispatched += auths
	if result.DispatchSyncTasks && shouldDispatchNextTask(msg.MessageType) {
		dispatched, result, err = s.dispatchSyncTasksInteractively(conn, session, reqID, result)
		if err != nil {
			return dispatched, err
		}
		dispatched += auths
	}
	return dispatched, s.writeMessageAck(conn, session, reqID, msg, result.HeartbeatResult)
}
