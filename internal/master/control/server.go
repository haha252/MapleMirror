package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type ControlServer struct {
	Repo              Repository
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	Logger            *logging.Logger
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
	remote := conn.RemoteAddr().String()
	if s.Logger != nil {
		s.Logger.Debug(context.Background(), "控制会话开始",
			slog.String("request_id", reqID),
			slog.String("node_fingerprint", fp),
			slog.String("remote_addr", remote))
	}
	session, err := s.Repo.StartSession(context.Background(), fp, reqID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Debug(context.Background(), "控制会话建立失败",
				slog.String("request_id", reqID),
				slog.String("node_fingerprint", fp),
				slog.String("remote_addr", remote),
				slog.String("error", err.Error()))
		}
		return
	}
	closeReason := "连接关闭"
	defer func() {
		_ = s.Repo.CloseSession(context.Background(), session.ID, closeReason)
		if s.Logger != nil {
			s.Logger.Debug(context.Background(), "控制会话结束",
				slog.String("request_id", reqID),
				slog.String("session_id", session.ID),
				slog.String("node_id", session.NodeID),
				slog.String("reason", closeReason))
		}
	}()
	if s.Logger != nil {
		s.Logger.Debug(context.Background(), "节点会话已建立",
			slog.String("request_id", reqID),
			slog.String("session_id", session.ID),
			slog.String("node_id", session.NodeID),
			slog.String("fingerprint", fp),
			slog.String("remote_addr", remote))
	}
	if !s.readHello(conn, session, reqID) {
		closeReason = "hello 读取失败"
		return
	}
	if s.Logger != nil {
		s.Logger.Debug(context.Background(), "已向节点发送欢迎信息",
			slog.String("request_id", reqID),
			slog.String("session_id", session.ID),
			slog.String("node_id", session.NodeID),
			slog.Int("heartbeat_interval_seconds", int(s.HeartbeatInterval.Seconds())),
			slog.Int("heartbeat_timeout_seconds", int(s.HeartbeatTimeout.Seconds())))
	}
	for {
		msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil {
			closeReason = err.Error()
			if s.Logger != nil {
				s.Logger.Debug(context.Background(), "控制会话读取结束",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("error", err.Error()))
			}
			return
		}
		if msg.NodeID != session.NodeID {
			closeReason = "节点标识不匹配"
			if s.Logger != nil {
				s.Logger.Warn(context.Background(), "控制消息节点标识不匹配",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("expected_node_id", session.NodeID),
					slog.String("message_node_id", msg.NodeID))
			}
			return
		}
		if s.Logger != nil {
			s.Logger.Debug(context.Background(), "收到控制消息",
				slog.String("request_id", reqID),
				slog.String("session_id", session.ID),
				slog.String("node_id", session.NodeID),
				slog.String("message_type", msg.MessageType),
				slog.Uint64("sequence", msg.Sequence))
		}
		result, err := s.handleMessage(session, msg)
		if err != nil {
			closeReason = err.Error()
			if s.Logger != nil {
				s.Logger.Debug(context.Background(), "控制消息处理失败",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("message_type", msg.MessageType),
					slog.String("error", err.Error()))
			}
			return
		}
		if s.Logger != nil {
			switch msg.MessageType {
			case protocol.TypeHeartbeat:
				s.Logger.Debug(context.Background(), "节点心跳已记录",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("state", result.ManagedState),
					slog.Uint64("accepted_sequence", result.AcceptedSequence))
			case protocol.TypeInventoryReport:
				s.Logger.Debug(context.Background(), "节点库存已记录",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("state", result.ManagedState),
					slog.Uint64("accepted_sequence", result.AcceptedSequence))
			case protocol.TypePressureReport:
				s.Logger.Debug(context.Background(), "节点压力已记录",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("state", result.ManagedState),
					slog.Uint64("accepted_sequence", result.AcceptedSequence))
			case protocol.TypeSyncTaskResult:
				s.Logger.Debug(context.Background(), "收到同步任务结果",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("task_id", msg.MessageID),
					slog.String("state", result.ManagedState),
					slog.Uint64("accepted_sequence", result.AcceptedSequence))
			case protocol.TypeTrafficEvent:
				s.Logger.Debug(context.Background(), "收到流量事件",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("state", result.ManagedState),
					slog.Uint64("accepted_sequence", result.AcceptedSequence))
			}
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
