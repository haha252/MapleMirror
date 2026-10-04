package control

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func runV2ClientWriter(ctx context.Context, conn *websocket.Conn, queue *controlv2.Queue) error {
	for {
		envelope, err := queue.Dequeue(ctx)
		if err != nil {
			return err
		}
		if err := writeV2ClientEnvelope(ctx, conn, envelope); err != nil {
			return err
		}
		queue.ReplaySent(envelope.ID)
	}
}

func readV2ClientEnvelope(ctx context.Context, conn *websocket.Conn) (protocolv2.Envelope, error) {
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		return protocolv2.Envelope{}, err
	}
	if messageType != websocket.MessageText {
		return protocolv2.Envelope{}, errors.New("control.v2 requires text JSON messages")
	}
	var envelope protocolv2.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return protocolv2.Envelope{}, err
	}
	if err := envelope.Validate(); err != nil {
		return protocolv2.Envelope{}, err
	}
	return envelope, nil
}

func writeV2ClientEnvelope(ctx context.Context, conn *websocket.Conn, envelope protocolv2.Envelope) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if len(data) > protocolv2.MaxMessageBytes {
		return errors.New("control.v2 message exceeds hard limit")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}
