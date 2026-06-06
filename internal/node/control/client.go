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

type Client struct {
	NodeID                string
	Address               string
	PublicDownloadBaseURL string
	Storage               string
	TargetBandwidthBPS    int64
	TLSConfig             *tls.Config
	HeartbeatInterval     time.Duration
	Logger                *logging.Logger
	Executor              interface {
		Execute(context.Context, protocol.SyncTask) protocol.SyncTaskResult
	}
	DB          *sql.DB
	TaskLimiter *TaskLimiter
}

func (c Client) RunOnce() (time.Duration, error) {
	if err := c.resetInterruptedLocalTasks(); err != nil {
		return 0, err
	}
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点开始连接主节点",
			slog.String("node_id", c.NodeID),
			slog.String("master", c.Address))
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", c.Address, c.TLSConfig)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Debug(context.Background(), "节点连接主节点失败",
				slog.String("node_id", c.NodeID),
				slog.String("master", c.Address),
				slog.String("error", err.Error()))
		}
		return 0, err
	}
	defer conn.Close()
	reqID, _ := requestid.New()
	if err := c.hello(conn, reqID); err != nil {
		return 0, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(controlIOTimeout))
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return 0, err
	}
	if msg.MessageType == protocol.TypeProtocolError {
		return 0, parseRejectionError(msg)
	}
	var welcome protocol.Welcome
	if msg.MessageType != protocol.TypeWelcome {
		return 0, errors.New("主节点返回了非预期的控制响应")
	}
	if err := json.Unmarshal(msg.Payload, &welcome); err != nil {
		return 0, err
	}
	interval := time.Duration(welcome.HeartbeatIntervalSecond) * time.Second
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点收到主节点欢迎信息",
			slog.String("node_id", c.NodeID),
			slog.String("master", c.Address),
			slog.Int("heartbeat_interval_seconds", welcome.HeartbeatIntervalSecond),
			slog.Int("heartbeat_timeout_seconds", welcome.HeartbeatTimeoutSecond),
			slog.String("managed_state", welcome.ManagedState),
			slog.Bool("routing_ready", welcome.RoutingReady))
	}
	if err := c.heartbeat(conn, reqID, 2); err != nil {
		return interval, err
	}
	if err := c.sendPressureReport(conn, reqID, 3); err != nil {
		return interval, err
	}
	nextSeq, err := c.sendPendingTraffic(conn, reqID, 4)
	if err != nil {
		return interval, err
	}
	nextSeq, err = c.sendPendingTaskResults(conn, reqID, nextSeq)
	if err != nil {
		return interval, err
	}
	nextSeq, err = c.sendRunningTaskAcks(conn, reqID, nextSeq)
	if err != nil {
		return interval, err
	}
	nextSeq, err = c.sendFullInventoryReport(conn, reqID, nextSeq)
	if err != nil {
		return interval, err
	}
	if err := c.readOptionalTask(conn, reqID, nextSeq); err != nil {
		return interval, err
	}
	return interval, nil
}

func (c Client) resetInterruptedLocalTasks() error {
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`UPDATE local_sync_tasks SET state = 'interrupted',
		error_message = '控制连接重新建立后停止续报运行状态',
		updated_at = ? WHERE state = 'running'`,
		time.Now().UTC().Format(time.RFC3339Nano))
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

func (c Client) heartbeat(conn net.Conn, reqID string, sequence uint64) error {
	active := c.activeDownloads()
	body, _ := json.Marshal(protocol.Heartbeat{
		Status: "syncing", ActiveDownloads: active, FreeBytes: 0,
		PublicDownloadBaseURL: c.PublicDownloadBaseURL,
		Pressure: protocol.PressureSample{
			TargetBandwidthBPS: c.TargetBandwidthBPS,
			ActualBandwidthBPS: 0,
			Ratio:              pressureRatio(0, c.TargetBandwidthBPS),
		},
	})
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点发送心跳",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.Uint64("sequence", sequence))
	}
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
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点收到心跳确认",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("message_type", msg.MessageType))
	}
	return nil
}

func (c Client) readOptionalTask(conn net.Conn, reqID string, sequence uint64) error {
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return nil
	}
	if msg.MessageType == protocol.TypeProtocolError {
		return parseRejectionError(msg)
	}
	if msg.MessageType != protocol.TypeSyncTask {
		return nil
	}
	task, err := c.decodeSyncTask(msg, reqID)
	if err != nil {
		return err
	}
	if c.Executor == nil {
		c.logSyncExecutorDisabled(reqID, task.TaskID)
		return c.sendTaskResult(conn, reqID, sequence, disabledExecutorResult(task))
	}
	if err := c.sendTaskAck(conn, reqID, sequence, task); err != nil {
		return err
	}
	c.executeTaskAsync(task)
	return nil
}

func (c Client) sendTaskAck(conn net.Conn, reqID string, sequence uint64, task protocol.SyncTask) error {
	body, _ := json.Marshal(protocol.SyncTaskAck{
		TaskID: task.TaskID, State: "running",
	})
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点确认同步任务开始执行",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID))
	}
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID + "-task-ack",
		MessageType: protocol.TypeSyncTaskAck, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return err
	}
	_, err := c.readExpectedResponse(conn, reqID, protocol.TypeHeartbeatAck)
	return err
}

func (c Client) sendTaskResult(conn net.Conn, reqID string, sequence uint64, result protocol.SyncTaskResult) error {
	body, _ := json.Marshal(result)
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点回传同步任务结果",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", result.TaskID),
			slog.String("asset_id", result.AssetID),
			slog.String("result", result.Result))
	}
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID + "-task-result",
		MessageType: protocol.TypeSyncTaskResult, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return err
	}
	_, err := c.readExpectedResponse(conn, reqID, protocol.TypeHeartbeatAck)
	return err
}
