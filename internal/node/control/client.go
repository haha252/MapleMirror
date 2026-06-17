package control

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/logging"
	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

const controlIOTimeout = 10 * time.Second

const defaultSyncTaskTimeout = 30 * time.Minute

type Client struct {
	NodeID                string
	Address               string
	PublicDownloadBaseURL string
	Storage               string
	TargetBandwidthBPS    int64
	MaxMirrorProjects     int
	TLSConfig             *tls.Config
	HeartbeatInterval     time.Duration
	Logger                *logging.Logger
	Executor              interface {
		Execute(context.Context, protocol.SyncTask) protocol.SyncTaskResult
	}
	DB          *sql.DB
	TaskLimiter *TaskLimiter
	TaskTimeout time.Duration
	Bandwidth   BandwidthSampler
	ProbeStore  interface {
		Accept(protocol.PublicProbeChallenge) error
	}
	DialTLSContext func(context.Context, string, string, *tls.Config) (net.Conn, error)
}

func (c Client) syncTaskTimeout() time.Duration {
	if c.TaskTimeout > 0 {
		return c.TaskTimeout
	}
	return defaultSyncTaskTimeout
}

func (c Client) RunOnce() (time.Duration, error) {
	if err := c.resetInterruptedLocalTasks(); err != nil {
		return 0, err
	}
	c.logDebug("node control connection starting",
		slog.String("node_id", c.NodeID), slog.String("master", c.Address))
	dialTLS := c.DialTLSContext
	if dialTLS == nil {
		dialTLS = func(ctx context.Context, network, address string, cfg *tls.Config) (net.Conn, error) {
			_ = ctx
			return tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second},
				network, address, cfg)
		}
	}
	conn, err := dialTLS(context.Background(), "tcp", c.Address, c.TLSConfig)
	if err != nil {
		c.logDebug("node control connection failed",
			slog.String("node_id", c.NodeID), slog.String("master", c.Address),
			slog.String("error", err.Error()))
		return 0, err
	}
	defer conn.Close()
	reqID, _ := requestid.New()
	if err := c.hello(conn, reqID); err != nil {
		return 0, err
	}
	welcome, err := c.readWelcome(conn)
	if err != nil {
		return 0, err
	}
	interval := time.Duration(welcome.HeartbeatIntervalSecond) * time.Second
	c.logDebug("node received control welcome",
		slog.String("node_id", c.NodeID), slog.String("master", c.Address),
		slog.Int("heartbeat_interval_seconds", welcome.HeartbeatIntervalSecond),
		slog.Int("heartbeat_timeout_seconds", welcome.HeartbeatTimeoutSecond),
		slog.String("managed_state", welcome.ManagedState),
		slog.Bool("routing_ready", welcome.RoutingReady))
	if err := c.sendSessionReports(conn, reqID); err != nil {
		return interval, err
	}
	return interval, nil
}

func (c Client) readWelcome(conn net.Conn) (protocol.Welcome, error) {
	_ = conn.SetReadDeadline(time.Now().Add(controlIOTimeout))
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return protocol.Welcome{}, err
	}
	if msg.MessageType == protocol.TypeProtocolError {
		return protocol.Welcome{}, parseRejectionError(msg)
	}
	if msg.MessageType != protocol.TypeWelcome {
		return protocol.Welcome{}, errors.New("master returned unexpected control response")
	}
	var welcome protocol.Welcome
	if err := json.Unmarshal(msg.Payload, &welcome); err != nil {
		return protocol.Welcome{}, err
	}
	return welcome, nil
}

func (c Client) sendSessionReports(conn net.Conn, reqID string) error {
	actualBandwidth := c.sampleBandwidth()
	sequence := uint64(2)
	if err := c.heartbeat(conn, reqID, sequence, actualBandwidth); err != nil {
		return err
	}
	sequence++
	if err := c.sendPressureReport(conn, reqID, sequence, actualBandwidth); err != nil {
		return err
	}
	sequence++
	taskBudget := maxSyncTasksPerSession
	nextSeq, err := c.sendPendingTraffic(conn, reqID, sequence)
	if err != nil {
		return err
	}
	nextSeq, err = c.sendPendingTaskResults(conn, reqID, nextSeq, &taskBudget)
	if err != nil {
		return err
	}
	nextSeq, err = c.sendRunningTaskAcks(conn, reqID, nextSeq, &taskBudget)
	if err != nil {
		return err
	}
	nextSeq, err = c.sendFullInventoryReport(conn, reqID, nextSeq, &taskBudget)
	if err != nil {
		return err
	}
	_, err = c.readOptionalTasksWithBudget(conn, reqID, nextSeq, &taskBudget)
	return err
}

func (c Client) resetInterruptedLocalTasks() error {
	if c.DB == nil {
		return nil
	}
	cutoff := time.Now().UTC().Add(-c.syncTaskTimeout()).Format(time.RFC3339Nano)
	_, err := c.DB.Exec(`UPDATE local_sync_tasks SET state = 'interrupted',
		error_message = 'control connection restarted while task was running',
		updated_at = ? WHERE state = 'running' AND updated_at <= ?`,
		time.Now().UTC().Format(time.RFC3339Nano), cutoff)
	return err
}

func (c Client) hello(conn net.Conn, reqID string) error {
	body, _ := json.Marshal(map[string]any{
		"last_ack_sequence": 0,
		"capabilities":      []string{"heartbeat.v1", "inventory.report.v1", "pressure.report.v1"},
		"software_version":  "dev",
	})
	return c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeHello, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: 1, Payload: body,
	})
}

func (c Client) heartbeat(conn net.Conn, reqID string, sequence uint64,
	actualBandwidth int64) error {
	active := c.activeDownloads()
	body, _ := json.Marshal(protocol.Heartbeat{
		Status: "syncing", ActiveDownloads: active, FreeBytes: 0,
		PublicDownloadBaseURL: c.PublicDownloadBaseURL,
		MaxMirrorProjects:     c.MaxMirrorProjects,
		Pressure: protocol.PressureSample{
			TargetBandwidthBPS: c.TargetBandwidthBPS,
			ActualBandwidthBPS: actualBandwidth,
			Ratio:              pressureRatio(actualBandwidth, c.TargetBandwidthBPS),
		},
	})
	c.logDebug("node heartbeat sent", slog.String("node_id", c.NodeID),
		slog.String("request_id", reqID), slog.Uint64("sequence", sequence))
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeHeartbeat, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return err
	}
	msg, err := c.readExpectedResponse(conn, reqID, protocol.TypeHeartbeatAck)
	if err != nil {
		return err
	}
	if err := c.acceptPublicProbe(msg); err != nil {
		return err
	}
	c.logDebug("node heartbeat ack received", slog.String("node_id", c.NodeID),
		slog.String("request_id", reqID), slog.String("message_type", msg.MessageType))
	return nil
}

func (c Client) acceptPublicProbe(msg protocol.Envelope) error {
	if c.ProbeStore == nil {
		return nil
	}
	var ack protocol.HeartbeatAckPayload
	if err := json.Unmarshal(msg.Payload, &ack); err != nil {
		return err
	}
	if ack.PublicProbe == nil {
		return nil
	}
	return c.ProbeStore.Accept(*ack.PublicProbe)
}

func (c Client) logDebug(message string, attrs ...slog.Attr) {
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), message, attrs...)
	}
}
