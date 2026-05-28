package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/controltls"
	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type ControlServer struct {
	Repo              Repository
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
}

func (s ControlServer) Handle(conn net.Conn) {
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok || tlsConn.ConnectionState().PeerCertificates == nil {
		return
	}
	reqID, err := requestid.New()
	if err != nil {
		return
	}
	fp := controltls.Fingerprint(tlsConn.ConnectionState().PeerCertificates[0])
	session, err := s.Repo.StartSession(context.Background(), fp, reqID)
	if err != nil {
		return
	}
	defer s.Repo.CloseSession(context.Background(), session.ID, "连接关闭")
	if !s.readHello(conn, session, reqID) {
		return
	}
	for {
		msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil {
			return
		}
		if msg.NodeID != session.NodeID {
			return
		}
		result, err := s.handleMessage(session, msg)
		if err != nil {
			return
		}
		messageType := protocol.TypeHeartbeatAck
		payload := HeartbeatAck(result)
		if msg.MessageType == protocol.TypeTrafficEvent {
			messageType = protocol.TypeTrafficEventAck
			payload, _ = json.Marshal(protocol.TrafficEventAck{
				AcceptedSequence: result.AcceptedSequence,
				Message:          "流量事件已入账",
			})
		}
		_ = protocol.WriteFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version, MessageID: reqID,
			MessageType: messageType, SentAt: time.Now().UTC(),
			NodeID: session.NodeID, RequestID: reqID, ReplyTo: msg.MessageID,
			Payload: payload,
		})
		s.writeNextTask(conn, session, reqID)
	}
}

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
		return HeartbeatResult{AcceptedSequence: msg.Sequence, ManagedState: "syncing"}, nil
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
		return HeartbeatResult{}, context.Canceled
	}
}

func (s ControlServer) writeNextTask(conn net.Conn, session Session, reqID string) {
	task, ok, err := s.Repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		return
	}
	body, _ := json.Marshal(task)
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: task.TaskID,
		MessageType: protocol.TypeSyncTask, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, Payload: body,
	})
}

func (s ControlServer) readHello(conn net.Conn, session Session, reqID string) bool {
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil || msg.MessageType != protocol.TypeHello || msg.NodeID != session.NodeID {
		return false
	}
	body, _ := json.Marshal(protocol.Welcome{
		SessionID: session.ID, AcceptedSequence: session.AcceptedSequence,
		HeartbeatIntervalSecond: int(s.HeartbeatInterval.Seconds()),
		HeartbeatTimeoutSecond:  int(s.HeartbeatTimeout.Seconds()),
		ManagedState:            "syncing", RoutingReady: false,
	})
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeWelcome, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, ReplyTo: msg.MessageID,
		Payload: body,
	})
	return true
}
