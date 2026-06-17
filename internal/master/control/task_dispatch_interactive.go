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
	if result.SyncTaskSlotsKnown {
		s.Repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, result.SyncTaskSlotsAvailable)
	}
	if err := s.Repo.refreshExpiredSyncTaskLeases(context.Background(), session.NodeID); err != nil {
		return 0, result, err
	}
	dispatched := 0
	for {
		limit, err := s.syncTaskDispatchAllowance(session.NodeID)
		if err != nil || limit <= 0 {
			return dispatched, result, err
		}
		ok, err := s.writeNextTask(conn, session, reqID)
		if err != nil || !ok {
			return dispatched, result, err
		}
		dispatched++
		ack, ackResult, err := s.readDispatchedTaskResponse(conn, session, reqID)
		if err != nil {
			return dispatched, result, err
		}
		if err := s.writeMessageAck(conn, session, reqID, ack, ackResult.HeartbeatResult); err != nil {
			return dispatched, result, err
		}
		result = ackResult
		if result.SyncTaskSlotsKnown {
			s.Repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, result.SyncTaskSlotsAvailable)
		}
	}
}

func (s ControlServer) readDispatchedTaskResponse(conn net.Conn, session Session,
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
	if msg.MessageType != protocol.TypeSyncTaskAck && msg.MessageType != protocol.TypeSyncTaskResult {
		return msg, controlMessageResult{}, fmt.Errorf("期望同步任务响应，实际为 %s", msg.MessageType)
	}
	result, err := s.handleMessage(session, msg)
	return msg, result, err
}

func (s ControlServer) writeResponsesAfterMessage(conn net.Conn, session Session, reqID string,
	msg protocol.Envelope, result controlMessageResult) (int, error) {
	dispatched := 0
	if result.DispatchSyncTasks && shouldDispatchNextTask(msg.MessageType) {
		var err error
		dispatched, result, err = s.dispatchSyncTasksInteractively(conn, session, reqID, result)
		if err != nil {
			return dispatched, err
		}
	}
	return dispatched, s.writeMessageAck(conn, session, reqID, msg, result.HeartbeatResult)
}
