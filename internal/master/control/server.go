package control

import (
	"context"
	"crypto/tls"
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
	PublicProbes      *PublicProbeService
}

func (s ControlServer) Handle(conn net.Conn) {
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		if s.Logger != nil {
			s.Logger.Debug(context.Background(), "控制会话收到非 TLS 连接",
				slog.String("remote_addr", conn.RemoteAddr().String()))
		}
		return
	}
	remote := conn.RemoteAddr().String()
	reqID, err := requestid.New()
	if err != nil {
		return
	}
	if err := tlsConn.Handshake(); err != nil {
		if s.Logger != nil {
			s.Logger.Debug(context.Background(), "控制会话 TLS 握手失败",
				slog.String("request_id", reqID),
				slog.String("remote_addr", remote),
				slog.String("error", err.Error()))
		}
		return
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		if s.Logger != nil {
			s.Logger.Debug(context.Background(), "控制会话未提供客户端证书",
				slog.String("request_id", reqID),
				slog.String("remote_addr", remote))
		}
		return
	}
	fp := controltls.Fingerprint(state.PeerCertificates[0])
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
		s.writeStartSessionReject(conn, tlsConn, reqID, fp, err)
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
	if err := s.readHello(conn, session, reqID); err != nil {
		closeReason = "hello 交换失败: " + err.Error()
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
		msg, err := readControlFrame(conn, s.sessionReadTimeout())
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
			s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
				"NODE_ID_MISMATCH", closeReason)
			if s.Logger != nil {
				s.Logger.Warn(context.Background(), "控制消息节点标识不匹配",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("expected_node_id", session.NodeID),
					slog.String("message_node_id", msg.NodeID))
			}
			return
		}
		if err := msg.Validate(protocol.Control); err != nil {
			closeReason = "控制消息无效: " + err.Error()
			s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
				"CONTROL_PROTOCOL_ERROR", closeReason)
			if s.Logger != nil {
				s.Logger.Debug(context.Background(), "控制消息校验失败",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("message_type", msg.MessageType),
					slog.String("error", err.Error()))
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
			s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
				"CONTROL_MESSAGE_ERROR", err.Error())
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
		if err := s.writeMessageAck(conn, session, reqID, msg, result); err != nil {
			closeReason = "控制响应发送失败: " + err.Error()
			if s.Logger != nil {
				s.Logger.Debug(context.Background(), "控制响应发送失败",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("error", err.Error()))
			}
			return
		}
		if !shouldDispatchNextTask(msg.MessageType) {
			continue
		}
		if err := s.writeNextTask(conn, session, reqID); err != nil {
			closeReason = "同步任务下发失败: " + err.Error()
			if s.Logger != nil {
				s.Logger.Debug(context.Background(), "同步任务下发失败",
					slog.String("request_id", reqID),
					slog.String("session_id", session.ID),
					slog.String("node_id", session.NodeID),
					slog.String("error", err.Error()))
			}
			return
		}
	}
}
