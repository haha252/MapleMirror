package control

import (
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (s ControlServer) writeMessageAck(conn net.Conn, session Session, reqID string,
	msg protocol.Envelope, result HeartbeatResult) error {
	messageType := protocol.TypeHeartbeatAck
	payload := HeartbeatAck(result)
	if msg.MessageType == protocol.TypeTrafficEvent {
		messageType = protocol.TypeTrafficEventAck
		payload, _ = json.Marshal(protocol.TrafficEventAck{
			AcceptedSequence: result.AcceptedSequence,
			Message:          "流量事件已入账",
		})
	}
	return writeControlFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: messageType, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, ReplyTo: msg.MessageID,
		Payload: payload,
	})
}
