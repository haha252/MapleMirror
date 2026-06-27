package control

import (
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) sendPublicProbeReady(conn net.Conn, reqID string, sequence uint64,
	challengeID string) (uint64, error) {
	if challengeID == "" {
		return sequence, nil
	}
	body, _ := json.Marshal(protocol.PublicProbeReady{ChallengeID: challengeID})
	messageID := reqID + "-public-probe-ready"
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: messageID,
		MessageType: protocol.TypePublicProbeReady, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedAck(conn, reqID, &next,
		protocol.TypeHeartbeatAck, messageID)
	if err != nil {
		return sequence, err
	}
	return next, nil
}
