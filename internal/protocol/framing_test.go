package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

type temporaryReadTimeout struct{}

func (temporaryReadTimeout) Error() string   { return "temporary read timeout" }
func (temporaryReadTimeout) Timeout() bool   { return true }
func (temporaryReadTimeout) Temporary() bool { return true }

type interruptingReader struct {
	data        []byte
	offset      int
	interruptAt int
	interrupted bool
}

func (r *interruptingReader) Read(p []byte) (int, error) {
	if !r.interrupted && r.offset >= r.interruptAt {
		r.interrupted = true
		return 0, temporaryReadTimeout{}
	}
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := len(p)
	if remaining := len(r.data) - r.offset; n > remaining {
		n = remaining
	}
	if !r.interrupted && r.offset+n > r.interruptAt {
		n = r.interruptAt - r.offset
	}
	copy(p, r.data[r.offset:r.offset+n])
	r.offset += n
	return n, nil
}

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

func TestFrameReaderResumesAfterPartialReadTimeout(t *testing.T) {
	source := Envelope{
		ProtocolVersion: Version, MessageID: "msg-1", MessageType: TypeHeartbeat,
		SentAt: time.Now().UTC(), NodeID: "node-1", RequestID: "req-1",
		Sequence: 3, Payload: json.RawMessage(`{"ok":true}`),
	}
	var encoded bytes.Buffer
	if err := WriteFrame(&encoded, source); err != nil {
		t.Fatal(err)
	}
	for _, interruptAt := range []int{2, 12} {
		t.Run(fmt.Sprintf("byte_%d", interruptAt), func(t *testing.T) {
			reader := &interruptingReader{data: encoded.Bytes(), interruptAt: interruptAt}
			frames := NewFrameReader(MaxFrameBytes)
			if _, err := frames.ReadFrame(reader); err == nil {
				t.Fatal("expected temporary timeout")
			}
			got, err := frames.ReadFrame(reader)
			if err != nil {
				t.Fatal(err)
			}
			if got.MessageID != source.MessageID || got.Sequence != source.Sequence {
				t.Fatalf("resumed frame = %+v", got)
			}
		})
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
