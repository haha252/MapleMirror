package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestFrameRoundTripAndValidate(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"ok": "是"})
	source := Envelope{
		ProtocolVersion: Version,
		MessageID:       "msg-1", MessageType: TypeHeartbeat,
		SentAt: time.Now().UTC(), NodeID: "node-1",
		RequestID: "req-1", Sequence: 3, Payload: payload,
	}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, source); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf, MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(Control); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentRejectsControlOnlyMessage(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"ok": "否"})
	envelope := Envelope{
		ProtocolVersion: Version, MessageID: "msg-1",
		MessageType: TypeHeartbeat, SentAt: time.Now().UTC(),
		RequestID: "req-1", Payload: payload,
	}
	if err := envelope.Validate(Enrollment); err == nil {
		t.Fatal("登记通道不得接受心跳消息")
	}
}
