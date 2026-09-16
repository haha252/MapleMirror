package v2

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRejectsUnknownMessageType(t *testing.T) {
	e := Envelope{Version: Version, Type: "future.unknown", ID: "id-1", Payload: json.RawMessage(`{}`)}
	if err := e.Validate(); err == nil {
		t.Fatal("unknown control.v2 message type should be rejected")
	}
}
