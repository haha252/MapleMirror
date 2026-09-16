package control

import (
	"fmt"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (c *Client) handleV2BusinessInbound(queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	switch envelope.Type {
	case protocolv2.TypeDownloadAuthorization:
		return c.handleV2DownloadAuthorization(queue, envelope)
	case protocolv2.TypeTrafficEventAck:
		return c.handleV2TrafficAck(envelope)
	case protocolv2.TypeAuthorizationStatusAck:
		return c.handleV2AuthorizationStatusAck(envelope)
	case protocolv2.TypePublicProbeChallenge:
		return c.handleV2PublicProbe(queue, envelope)
	case protocolv2.TypeSwarmManifestAck:
		return c.handleV2ManifestAck(queue, envelope)
	case protocolv2.TypeSwarmSources:
		return c.handleV2Sources(envelope)
	default:
		return fmt.Errorf("unsupported control.v2 inbound message %s", envelope.Type)
	}
}
