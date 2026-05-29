package control

import (
	"context"
	"crypto/tls"
	"database/sql"
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
	Executor          interface {
		Execute(context.Context, protocol.SyncTask) protocol.SyncTaskResult
	}
	DB *sql.DB
}

func (c Client) RunOnce() (time.Duration, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", c.Address, c.TLSConfig)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	reqID, _ := requestid.New()
	if err := c.hello(conn, reqID); err != nil {
		return 0, err
	}
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil {
		return 0, err
	}
	var welcome protocol.Welcome
	if msg.MessageType != protocol.TypeWelcome {
		return 0, nil
	}
	if err := json.Unmarshal(msg.Payload, &welcome); err != nil {
		return 0, err
	}
	interval := time.Duration(welcome.HeartbeatIntervalSecond) * time.Second
	if err := c.heartbeat(conn, reqID, 2); err != nil {
		return interval, err
	}
	nextSeq, err := c.sendPendingTraffic(conn, reqID, 3)
	if err != nil {
		return interval, err
	}
	if err := c.readOptionalTask(conn, reqID, nextSeq); err != nil {
		return interval, err
	}
	return interval, nil
}

func (c *Client) Run(stop <-chan struct{}) {
	interval := c.HeartbeatInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if nextInterval, err := c.RunOnce(); err == nil && nextInterval > 0 {
			interval = nextInterval
			c.HeartbeatInterval = nextInterval
		}
		select {
		case <-stop:
			return
		case <-time.After(interval):
		}
	}
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

func (c Client) readOptionalTask(conn net.Conn, reqID string, sequence uint64) error {
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || msg.MessageType != protocol.TypeSyncTask {
		return nil
	}
	var task protocol.SyncTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		return err
	}
	if c.Executor == nil {
		return c.sendTaskResult(conn, reqID, sequence, protocol.SyncTaskResult{
			TaskID: task.TaskID, AssetID: task.Asset.AssetID,
			Result: "temporary_error", Message: "节点同步执行器未启用",
		})
	}
	result := c.Executor.Execute(context.Background(), task)
	return c.sendTaskResult(conn, reqID, sequence, result)
}

func (c Client) sendTaskResult(conn net.Conn, reqID string, sequence uint64, result protocol.SyncTaskResult) error {
	body, _ := json.Marshal(result)
	if err := protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID + "-task-result",
		MessageType: protocol.TypeSyncTaskResult, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return err
	}
	_, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	return err
}
