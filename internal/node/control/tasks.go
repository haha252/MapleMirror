package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const maxSyncTasksPerSession = 10

func (c Client) readOptionalTasks(conn net.Conn, reqID string, sequence uint64,
	remaining int) (uint64, error) {
	for remaining > 0 {
		next, handled, err := c.readOptionalTaskWithTimeout(conn, reqID,
			sequence, nil, 200*time.Millisecond)
		if err != nil {
			return sequence, nil
		}
		if !handled {
			return sequence, nil
		}
		sequence = next
		remaining--
	}
	return sequence, nil
}

func (c Client) readOptionalTasksWithBudget(conn net.Conn, reqID string,
	sequence uint64, budget *int) (uint64, error) {
	if budget == nil {
		return c.readOptionalTasks(conn, reqID, sequence, maxSyncTasksPerSession)
	}
	if *budget <= 0 {
		return sequence, nil
	}
	next, err := c.readOptionalTasks(conn, reqID, sequence, *budget)
	*budget -= int(next - sequence)
	return next, err
}

func (c Client) readOptionalTaskWithTimeout(conn net.Conn, reqID string,
	sequence uint64, budget *int, timeout time.Duration) (uint64, bool, error) {
	if budget != nil && *budget <= 0 {
		return sequence, false, nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		if timeoutErr, ok := err.(net.Error); ok && timeoutErr.Timeout() {
			return sequence, false, nil
		}
		return sequence, false, err
	}
	if msg.MessageType == protocol.TypeProtocolError {
		return sequence, false, parseRejectionError(msg)
	}
	if msg.MessageType != protocol.TypeSyncTask {
		return sequence, false, nil
	}
	next, err := c.handleDispatchedTask(conn, reqID, sequence, msg)
	if err != nil {
		return sequence, false, err
	}
	if budget != nil {
		*budget = *budget - 1
	}
	return next, true, nil
}

func (c Client) handleDispatchedTask(conn net.Conn, reqID string, sequence uint64,
	msg protocol.Envelope) (uint64, error) {
	task, err := c.decodeSyncTask(msg, reqID)
	if err != nil {
		return sequence, err
	}
	if c.Executor == nil {
		c.logSyncExecutorDisabled(reqID, task.TaskID)
		if err := c.sendTaskResult(conn, reqID, sequence, disabledExecutorResult(task)); err != nil {
			return sequence, err
		}
		return sequence + 1, nil
	}
	if err := c.sendTaskAck(conn, reqID, sequence, task); err != nil {
		return sequence, err
	}
	c.executeTaskAsync(task)
	return sequence + 1, nil
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
