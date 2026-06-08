package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

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

func (c Client) sendTaskAck(conn net.Conn, reqID string, sequence uint64,
	task protocol.SyncTask) error {
	body, _ := json.Marshal(protocol.SyncTaskAck{
		TaskID: task.TaskID, State: "running",
	})
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "node sync task ack sent",
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

func (c Client) sendTaskResult(conn net.Conn, reqID string, sequence uint64,
	result protocol.SyncTaskResult) error {
	body, _ := json.Marshal(result)
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "node sync task result sent",
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
