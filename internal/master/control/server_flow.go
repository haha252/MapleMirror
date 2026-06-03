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

func (s ControlServer) handleMessage(session Session, msg protocol.Envelope) (HeartbeatResult, error) {
	switch msg.MessageType {
	case protocol.TypeHeartbeat:
		var hb protocol.Heartbeat
		if err := json.Unmarshal(msg.Payload, &hb); err != nil {
			return HeartbeatResult{}, err
		}
		return s.Repo.AcceptHeartbeat(context.Background(), session, msg.Sequence, hb)
	case protocol.TypeInventoryReport:
		var report protocol.InventoryReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			return HeartbeatResult{}, err
		}
		return s.Repo.AcceptInventoryReport(context.Background(), session, msg.Sequence, report)
	case protocol.TypePressureReport:
		var report protocol.PressureReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			return HeartbeatResult{}, err
		}
		return s.Repo.AcceptPressureReport(context.Background(), session, msg.Sequence, report)
	case protocol.TypeSyncTaskAck:
		ready := s.Repo.nodeRoutingReady(context.Background(), session.NodeID)
		return HeartbeatResult{AcceptedSequence: msg.Sequence, ManagedState: managedState(ready), RoutingReady: ready}, nil
	case protocol.TypeSyncTaskResult:
		var result protocol.SyncTaskResult
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			return HeartbeatResult{}, err
		}
		return s.Repo.AcceptSyncTaskResult(context.Background(), session, msg.Sequence, result)
	case protocol.TypeTrafficEvent:
		var event protocol.TrafficEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return HeartbeatResult{}, err
		}
		return s.Repo.AcceptTrafficEvent(context.Background(), session, msg.Sequence, event)
	default:
		return HeartbeatResult{}, fmt.Errorf("不支持的控制消息类型: %s", msg.MessageType)
	}
}

func (s ControlServer) writeNextTask(conn net.Conn, session Session, reqID string) error {
	task, ok, err := s.Repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		if err != nil && s.Logger != nil {
			s.Logger.Debug(context.Background(), "查询待下发同步任务失败",
				slog.String("request_id", reqID),
				slog.String("node_id", session.NodeID),
				slog.String("error", err.Error()))
		}
		return err
	}
	if s.Logger != nil {
		s.Logger.Debug(context.Background(), "向节点下发同步任务",
			slog.String("request_id", reqID),
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("node_id", session.NodeID),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("file_name", task.Asset.FileName),
			slog.Int64("size_bytes", task.Asset.SizeBytes))
	}
	body, _ := json.Marshal(task)
	return writeControlFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: task.TaskID,
		MessageType: protocol.TypeSyncTask, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, Payload: body,
	})
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
