package control

import (
	"net"
	"sync"
	"time"

	"mirror-server/internal/protocol"
)

const controlWriteTimeout = 10 * time.Second

type serializedConn struct {
	net.Conn
	writeMu sync.Mutex
}

func (c *serializedConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.Conn.Write(p)
}

func (c *serializedConn) writeFrame(envelope protocol.Envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return protocol.WriteFrame(c.Conn, envelope)
}

func writeControlFrame(conn net.Conn, envelope protocol.Envelope) error {
	_ = conn.SetWriteDeadline(time.Now().Add(controlWriteTimeout))
	var err error
	if serialized, ok := conn.(*serializedConn); ok {
		err = serialized.writeFrame(envelope)
	} else {
		err = protocol.WriteFrame(conn, envelope)
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func readControlFrame(conn net.Conn, timeout time.Duration,
	readers ...*protocol.FrameReader) (protocol.Envelope, error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
	}
	var msg protocol.Envelope
	var err error
	if len(readers) > 0 && readers[0] != nil {
		msg, err = readers[0].ReadFrame(conn)
	} else {
		msg, err = protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return msg, err
}
