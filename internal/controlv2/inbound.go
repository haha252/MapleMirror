package controlv2

import (
	"context"
	"fmt"

	"github.com/coder/websocket"
	protocolv2 "mirror-server/internal/protocol/v2"
)

const InboundMaxMessages = 128
const InboundMaxBytes = 8 << 20

type InboundBacklogError struct {
	Messages    int
	Bytes       int
	MessageType string
}

func (e *InboundBacklogError) Error() string {
	return fmt.Sprintf("control.v2 inbound backlog exceeded: messages=%d/%d bytes=%d/%d incoming=%s",
		e.Messages, InboundMaxMessages, e.Bytes, InboundMaxBytes, e.MessageType)
}

// ReadStream keeps WebSocket control frames moving while ordered business
// handlers wait for SQLite or disk. Bound memory and close on overload so
// durable messages can replay on reconnect instead of starving ping/pong.
func ReadStream(ctx context.Context, conn *websocket.Conn, read func(context.Context, *websocket.Conn) (protocolv2.Envelope, error)) (<-chan protocolv2.Envelope, <-chan error) {
	messages := make(chan protocolv2.Envelope, InboundMaxMessages)
	failures := make(chan error, 1)
	go func() {
		// Only this goroutine writes the channel. Reconcile the FIFO sizes with
		// its current length to release byte reservations consumed by handlers.
		var sizes []int
		bytes := 0
		for {
			envelope, err := read(ctx, conn)
			if err != nil {
				failures <- err
				return
			}
			consumed := len(sizes) - len(messages)
			for _, size := range sizes[:consumed] {
				bytes -= size
			}
			sizes = sizes[consumed:]
			size := len(envelope.Payload) + len(envelope.ID) + len(envelope.ReplyTo) + len(envelope.Type) + 256
			if bytes+size > InboundMaxBytes {
				failures <- &InboundBacklogError{len(messages), bytes + size, envelope.Type}
				_ = conn.CloseNow()
				return
			}
			select {
			case messages <- envelope:
				sizes = append(sizes, size)
				bytes += size
			case <-ctx.Done():
				failures <- ctx.Err()
				return
			default:
				failures <- &InboundBacklogError{len(messages), bytes + size, envelope.Type}
				_ = conn.CloseNow()
				return
			}
		}
	}()
	return messages, failures
}
