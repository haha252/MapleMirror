package control

import (
	"fmt"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func requireV2MessageID(envelope protocolv2.Envelope, messageType string, parts ...string) error {
	expected := protocolv2.StableMessageID(messageType, parts...)
	if envelope.ID != expected {
		return fmt.Errorf("%s message id mismatch", messageType)
	}
	return nil
}

func requireV2ReplyTo(envelope protocolv2.Envelope, requestType string, parts ...string) error {
	expected := protocolv2.StableMessageID(requestType, parts...)
	if envelope.ReplyTo != expected {
		return fmt.Errorf("%s reply_to mismatch", envelope.Type)
	}
	return nil
}
