package control

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type Client struct {
	NodeID            string
	Address           string
	TLSConfig         *tls.Config
	HeartbeatInterval time.Duration
}

func (c Client) RunOnce() error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", c.Address, c.TLSConfig)
	if err != nil {
		return err
	}
	defer conn.Close()
	reqID, _ := requestid.New()
	if err := c.hello(conn, reqID); err != nil {
		return err
	}
	if _, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes); err != nil {
		return err
	}
	return c.heartbeat(conn, reqID, 2)
}

func (c Client) hello(conn net.Conn, reqID string) error {
	body, _ := json.Marshal(map[string]any{
		"last_ack_sequence": 0,
		"capabilities":      []string{"heartbeat.v1", "inventory.report.v1", "pressure.report.v1"},
		"software_version":  "dev",
	})
	return protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeHello, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: 1, Payload: body,
	})
}

func (c Client) heartbeat(conn net.Conn, reqID string, sequence uint64) error {
	body, _ := json.Marshal(protocol.Heartbeat{
		Status: "syncing", ActiveDownloads: 0, FreeBytes: 0,
		Pressure: protocol.PressureSample{},
	})
	if err := protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeHeartbeat, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return err
	}
	_, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	return err
}
