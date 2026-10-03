package controlv2

import (
	"context"
	"errors"

	"github.com/coder/websocket"
	protocolv2 "mirror-server/internal/protocol/v2"
)

// ReadStream keeps WebSocket control frames moving while ordered business
// handlers wait for SQLite or disk. Bound memory and close on overload so
// durable messages can replay on reconnect instead of starving ping/pong.
func ReadStream(ctx context.Context, conn *websocket.Conn, read func(context.Context, *websocket.Conn) (protocolv2.Envelope, error)) (<-chan protocolv2.Envelope, <-chan error) {
	messages := make(chan protocolv2.Envelope, 8)
	failures := make(chan error, 1)
	go func() {
		for {
			envelope, err := read(ctx, conn)
			if err != nil {
				failures <- err
				return
			}
			select {
			case messages <- envelope:
			case <-ctx.Done():
				failures <- ctx.Err()
				return
			default:
				failures <- errors.New("control.v2 inbound backlog exceeded")
				_ = conn.CloseNow()
				return
			}
		}
	}()
	return messages, failures
}
