package control

import (
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const controlWriteTimeout = 10 * time.Second

func writeControlFrame(conn net.Conn, envelope protocol.Envelope) error {
	_ = conn.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
	err := protocol.WriteFrame(conn, envelope)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func readControlFrame(conn net.Conn, timeout time.Duration) (protocol.Envelope, error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
	}
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	_ = conn.SetReadDeadline(time.Time{})
	return msg, err
}
