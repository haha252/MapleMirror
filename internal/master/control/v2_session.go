package control

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (s *V2Server) serveV2Session(parent context.Context, conn *websocket.Conn, session Session, queue *controlv2.Queue) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	started := time.Now()
	causes := make(chan error, 1)
	stop := func(err error) {
		select {
		case causes <- err:
		default:
		}
		cancel()
		_ = conn.CloseNow()
	}
	// Start reading before initial dispatch. A transport failure cancels pending
	// database work immediately, even when the business consumer is blocked.
	messages, readErrors := controlv2.ReadStream(ctx, conn, readV2Envelope)
	go func() {
		select {
		case err := <-readErrors:
			stop(fmt.Errorf("reader: %w", err))
		case <-ctx.Done():
		}
	}()
	go func() { stop(fmt.Errorf("writer: %w", runV2Writer(ctx, conn, queue))) }()
	go s.v2TaskWakeLoop(ctx, session, queue)
	for {
		select {
		case <-ctx.Done():
			select {
			case err := <-causes:
				return fmt.Errorf("lifetime=%s: %w", time.Since(started).Round(time.Millisecond), err)
			default:
				return ctx.Err()
			}
		case envelope := <-messages:
			if !s.isCurrentV2Connection(session.NodeID, conn) {
				return fmt.Errorf("control.v2 session replaced")
			}
			handling := time.Now()
			if err := s.handleV2Message(ctx, session, queue, envelope); err != nil {
				if ctx.Err() != nil {
					continue
				}
				if s.Logger != nil {
					s.Logger.Warn(ctx, "control.v2 消息处理失败", slog.String("node_id", session.NodeID),
						slog.String("session_id", session.ID), slog.String("type", envelope.Type), slog.String("error", err.Error()))
				}
				pe, _ := protocolv2.Reply(protocolv2.TypeProtocolError, mustID(), envelope.ID,
					protocolv2.ProtocolError{Code: "invalid_message", Message: err.Error()})
				if queueErr := queue.Enqueue(pe, ""); queueErr != nil {
					stop(fmt.Errorf("protocol error response: %w", queueErr))
				}
			}
			if elapsed := time.Since(handling); elapsed >= time.Second && s.Logger != nil {
				s.Logger.Warn(ctx, "control.v2 消息处理缓慢", slog.String("node_id", session.NodeID),
					slog.String("type", envelope.Type), slog.Duration("duration", elapsed))
			}
		}
	}
}
