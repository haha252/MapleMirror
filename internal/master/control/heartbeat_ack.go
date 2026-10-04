package control

import (
	"encoding/json"
	"time"

	"mirror-server/internal/protocol"
)

func HeartbeatAck(result HeartbeatResult) json.RawMessage {
	body, _ := json.Marshal(protocol.HeartbeatAckPayload{
		AcceptedSequence: result.AcceptedSequence,
		ServerTime:       time.Now().UTC(),
		ManagedState:     result.ManagedState,
		RoutingReady:     result.RoutingReady,
		PublicProbe:      result.PublicProbe,
	})
	return body
}
