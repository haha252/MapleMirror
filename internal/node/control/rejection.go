package control

import (
	"encoding/json"

	"mirror-server/internal/protocol"
)

type RejectionError struct {
	Code    string
	Message string
}

func (e RejectionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e RejectionError) Recoverable() bool {
	return e.Code == "CERTIFICATE_NOT_ACTIVE"
}

func parseRejectionError(msg protocol.Envelope) error {
	var payload protocol.ProtocolError
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return err
	}
	if payload.Code == "" {
		payload.Code = "CONTROL_PROTOCOL_ERROR"
	}
	return RejectionError{Code: payload.Code, Message: payload.Message}
}
