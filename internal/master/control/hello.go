package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

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
	var hello protocol.Hello
	if err := json.Unmarshal(msg.Payload, &hello); err != nil {
		s.writeProtocolError(conn, session.NodeID, reqID, msg.MessageID,
			"CONTROL_PROTOCOL_ERROR", "hello 载荷无效: "+err.Error())
		return err
	}
	s.Repo.runtime().SetSoftwareVersion(session.NodeID, normalizedSoftwareVersion(hello.SoftwareVersion))
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
