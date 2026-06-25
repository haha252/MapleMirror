package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

type controlMessageResult struct {
	HeartbeatResult
	SyncTaskSlotsAvailable int
	SyncTaskSlotsKnown     bool
	DispatchSyncTasks      bool
}

func (s ControlServer) handleMessage(session Session, msg protocol.Envelope) (controlMessageResult, error) {
	last, err := s.Repo.currentSequence(session)
	if err != nil {
		return controlMessageResult{}, err
	}
	shouldDispatch := msg.Sequence > last
	switch msg.MessageType {
	case protocol.TypeHeartbeat:
		var hb protocol.Heartbeat
		if err := json.Unmarshal(msg.Payload, &hb); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptHeartbeat(context.Background(), session, msg.Sequence, hb)
		out := controlMessageResult{HeartbeatResult: result, DispatchSyncTasks: shouldDispatch || result.SyncTasksChanged}
		if hb.SyncTaskSlotsAvailable != nil {
			out.SyncTaskSlotsAvailable = *hb.SyncTaskSlotsAvailable
			out.SyncTaskSlotsKnown = true
		}
		learnSyncTaskSlots(&out, session.NodeID, s.Repo.runtime())
		return out, err
	case protocol.TypeInventoryReport:
		var report protocol.InventoryReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptInventoryReport(context.Background(), session, msg.Sequence, report)
		out := controlMessageResult{HeartbeatResult: result, DispatchSyncTasks: shouldDispatch || result.SyncTasksChanged}
		if report.SyncTaskSlotsAvailable != nil {
			out.SyncTaskSlotsAvailable = *report.SyncTaskSlotsAvailable
			out.SyncTaskSlotsKnown = true
		}
		learnSyncTaskSlots(&out, session.NodeID, s.Repo.runtime())
		return out, err
	case protocol.TypePressureReport:
		var report protocol.PressureReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptPressureReport(context.Background(), session, msg.Sequence, report)
		out := controlMessageResult{HeartbeatResult: result, DispatchSyncTasks: shouldDispatch || result.SyncTasksChanged}
		if report.SyncTaskSlotsAvailable != nil {
			out.SyncTaskSlotsAvailable = *report.SyncTaskSlotsAvailable
			out.SyncTaskSlotsKnown = true
		}
		learnSyncTaskSlots(&out, session.NodeID, s.Repo.runtime())
		return out, err
	case protocol.TypeSyncTaskAck:
		var ack protocol.SyncTaskAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptSyncTaskAck(context.Background(), session, msg.Sequence, ack)
		out := controlMessageResult{HeartbeatResult: result, DispatchSyncTasks: shouldDispatch}
		if ack.SyncTaskSlotsAvailable != nil {
			out.SyncTaskSlotsAvailable = *ack.SyncTaskSlotsAvailable
			out.SyncTaskSlotsKnown = true
		}
		learnSyncTaskSlots(&out, session.NodeID, s.Repo.runtime())
		return out, err
	case protocol.TypeSyncTaskResult:
		var result protocol.SyncTaskResult
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			return controlMessageResult{}, err
		}
		hbResult, err := s.Repo.AcceptSyncTaskResult(context.Background(), session, msg.Sequence, result)
		out := controlMessageResult{HeartbeatResult: hbResult, DispatchSyncTasks: shouldDispatch || hbResult.SyncTasksChanged}
		if result.SyncTaskSlotsAvailable != nil {
			out.SyncTaskSlotsAvailable = *result.SyncTaskSlotsAvailable
			out.SyncTaskSlotsKnown = true
		}
		learnSyncTaskSlots(&out, session.NodeID, s.Repo.runtime())
		return out, err
	case protocol.TypeTrafficEvent:
		var event protocol.TrafficEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptTrafficEvent(context.Background(), session, msg.Sequence, event)
		return controlMessageResult{HeartbeatResult: result}, err
	case protocol.TypeDownloadAuthorizationAck:
		var ack protocol.DownloadAuthorizationAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptDownloadAuthorizationAck(context.Background(),
			session, msg.Sequence, ack)
		return controlMessageResult{HeartbeatResult: result}, err
	case protocol.TypeAuthorizationStatusEvent:
		var event protocol.AuthorizationStatusEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return controlMessageResult{}, err
		}
		result, err := s.Repo.AcceptAuthorizationStatusEvent(context.Background(),
			session, msg.Sequence, event)
		return controlMessageResult{HeartbeatResult: result}, err
	default:
		return controlMessageResult{}, fmt.Errorf("不支持的控制消息类型: %s", msg.MessageType)
	}
}

func (s ControlServer) writeNextTask(conn net.Conn, session Session, reqID string) (bool, error) {
	task, ok, err := s.Repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		if err != nil && s.Logger != nil {
			s.Logger.Debug(context.Background(), "查询待下发同步任务失败",
				slog.String("request_id", reqID),
				slog.String("node_id", session.NodeID),
				slog.String("error", err.Error()))
		}
		return false, err
	}
	if s.Logger != nil {
		s.Logger.Debug(context.Background(), "向节点下发同步任务",
			slog.String("request_id", reqID),
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("node_id", session.NodeID),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("file_name", task.Asset.FileName),
			slog.Int64("size_bytes", task.Asset.SizeBytes),
			slog.Int("fallback_sources", len(task.FallbackSources)))
	}
	body, _ := json.Marshal(task)
	if err := writeControlFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: task.TaskID,
		MessageType: protocol.TypeSyncTask, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, Payload: body,
	}); err != nil {
		if rollbackErr := s.Repo.rollbackSentSyncTask(context.Background(), session.NodeID,
			task.TaskID, "同步任务下发失败，等待重新派发: "+err.Error()); rollbackErr != nil && s.Logger != nil {
			s.Logger.Debug(context.Background(), "回收下发失败的同步任务失败",
				slog.String("request_id", reqID),
				slog.String("task_id", task.TaskID),
				slog.String("node_id", session.NodeID),
				slog.String("error", rollbackErr.Error()))
		}
		return false, err
	}
	return true, nil
}

func (s ControlServer) writeSyncTasks(conn net.Conn, session Session, reqID string) (int, error) {
	if err := s.Repo.refreshExpiredSyncTaskLeases(context.Background(), session.NodeID); err != nil {
		return 0, err
	}
	outstanding, err := s.Repo.outstandingSentSyncTasks(context.Background(), session.NodeID)
	if err != nil {
		return 0, err
	}
	limit := s.syncTaskDispatchAllowance(session.NodeID, outstanding)
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for dispatched < limit {
		ok, err := s.writeNextTask(conn, session, reqID)
		if err != nil || !ok {
			return dispatched, err
		}
		dispatched++
	}
	return dispatched, nil
}

func (s ControlServer) syncTaskDispatchAllowance(nodeID string, outstanding int) int {
	limit, known := s.Repo.runtime().SyncTaskDispatchCapacity(nodeID)
	if !known {
		limit = defaultSyncTaskDispatchWindow
	}
	if limit <= 0 {
		return 0
	}
	limit -= outstanding
	if limit < 0 {
		limit = 0
	}
	if limit > maxSyncTaskDispatchWindow {
		limit = maxSyncTaskDispatchWindow
	}
	return limit
}

func (s ControlServer) dispatchSyncTasksAfterMessage(conn net.Conn, session Session, reqID string, result controlMessageResult) (int, error) {
	if !result.DispatchSyncTasks {
		return 0, nil
	}
	return s.writeSyncTasks(conn, session, reqID)
}

func (s ControlServer) readHello(conn net.Conn, session Session, reqID string) error {
	msg, err := readControlFrame(conn, s.HeartbeatTimeout)
	if err != nil {
		return err
	}
	if err := msg.Validate(protocol.Control); err != nil {
		s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
			"CONTROL_PROTOCOL_ERROR", "hello 消息无效: "+err.Error())
		return err
	}
	if msg.NodeID != session.NodeID {
		err := fmt.Errorf("hello 节点标识不匹配")
		s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
			"NODE_ID_MISMATCH", err.Error())
		return err
	}
	if msg.MessageType != protocol.TypeHello {
		err := fmt.Errorf("期望 hello 消息，实际为 %s", msg.MessageType)
		s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
			"CONTROL_PROTOCOL_ERROR", err.Error())
		return err
	}
	ready := s.Repo.nodeRoutingReady(context.Background(), session.NodeID)
	body, _ := json.Marshal(protocol.Welcome{
		SessionID: session.ID, AcceptedSequence: session.AcceptedSequence,
		HeartbeatIntervalSecond: int(s.HeartbeatInterval.Seconds()),
		HeartbeatTimeoutSecond:  int(s.HeartbeatTimeout.Seconds()),
		ManagedState:            managedState(ready),
		RoutingReady:            ready,
	})
	return writeControlFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeWelcome, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, ReplyTo: msg.MessageID,
		Payload: body,
	})
}
