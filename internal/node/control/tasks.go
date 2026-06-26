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
const optionalTaskWaitTimeout = 200 * time.Millisecond
const optionalTaskReadyTimeout = 10 * time.Millisecond

func (c Client) readOptionalTasks(conn net.Conn, reqID string, sequence uint64,
	remaining int) (uint64, error) {
	return c.readOptionalTasksWithTimeout(conn, reqID, sequence, remaining, optionalTaskWaitTimeout)
}

func (c Client) readReadyOptionalTasks(conn net.Conn, reqID string,
	sequence uint64, remaining int) (uint64, error) {
	return c.readOptionalTasksWithTimeout(conn, reqID, sequence, remaining, optionalTaskReadyTimeout)
}

func (c Client) readOptionalTasksWithTimeout(conn net.Conn, reqID string, sequence uint64,
	remaining int, timeout time.Duration) (uint64, error) {
	for remaining > 0 {
		next, handled, err := c.readOptionalTaskWithTimeout(conn, reqID,
			sequence, timeout)
		if err != nil {
			return sequence, err
		}
		if !handled {
			return sequence, nil
		}
		sequence = next
		remaining--
	}
	return sequence, nil
}

func (c Client) readOptionalTasksToCapacity(conn net.Conn, reqID string,
	sequence uint64) (uint64, error) {
	available := c.availableSyncTaskSlots()
	if available <= 0 {
		return sequence, nil
	}
	return c.readOptionalTasks(conn, reqID, sequence, available)
}

func (c Client) readReadyOptionalTasksToCapacity(conn net.Conn, reqID string,
	sequence uint64) (uint64, error) {
	available := c.availableSyncTaskSlots()
	if available <= 0 {
		return sequence, nil
	}
	return c.readReadyOptionalTasks(conn, reqID, sequence, available)
}

func (c Client) readOptionalTaskWithTimeout(conn net.Conn, reqID string,
	sequence uint64, timeout time.Duration) (uint64, bool, error) {
	if c.Executor == nil && c.DB == nil {
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
	if msg.MessageType == protocol.TypeDownloadAuthorization {
		next, err := c.handleDownloadAuthorization(conn, reqID, sequence, msg)
		if err != nil {
			return sequence, false, err
		}
		return next, true, nil
	}
	if msg.MessageType != protocol.TypeSyncTask {
		return sequence, false, nil
	}
	next, err := c.handleDispatchedTask(conn, reqID, sequence, msg)
	if err != nil {
		return sequence, false, err
	}
	return next, true, nil
}

func (c Client) availableSyncTaskSlots() int {
	if c.Executor == nil {
		return 0
	}
	if c.TaskLimiter == nil {
		return maxSyncTasksPerSession
	}
	return c.TaskLimiter.Available()
}

func (c Client) reserveSyncTaskSlot() bool {
	if c.Executor == nil || c.TaskLimiter == nil {
		return true
	}
	return c.TaskLimiter.Reserve()
}

func (c Client) releaseSyncTaskSlot() {
	if c.Executor != nil && c.TaskLimiter != nil {
		c.TaskLimiter.Release()
	}
}

func (c Client) handleDispatchedTask(conn net.Conn, reqID string, sequence uint64,
	msg protocol.Envelope) (uint64, error) {
	task, err := c.decodeSyncTask(msg, reqID)
	if err != nil {
		return sequence, err
	}
	if c.Executor == nil {
		c.logSyncExecutorDisabled(reqID, task.TaskID)
		next, err := c.sendTaskResult(conn, reqID, sequence, disabledExecutorResult(task))
		if err != nil {
			return sequence, err
		}
		return next, nil
	}
	if !c.reserveSyncTaskSlot() {
		return c.sendTaskResult(conn, reqID, sequence, protocol.SyncTaskResult{
			TaskID: task.TaskID, AssetID: task.Asset.AssetID,
			Result: "temporary_error", Message: "节点同步执行槽已满",
		})
	}
	if err := c.recordAcceptedTask(task); err != nil {
		c.releaseSyncTaskSlot()
		return c.sendTaskResult(conn, reqID, sequence, protocol.SyncTaskResult{
			TaskID: task.TaskID, AssetID: task.Asset.AssetID,
			Result: "temporary_error", Message: "节点记录同步任务失败",
		})
	}
	next, err := c.sendTaskAck(conn, reqID, sequence, task)
	if err != nil {
		_ = c.revertAcceptedTask(task.TaskID)
		c.releaseSyncTaskSlot()
		return sequence, err
	}
	c.executeTaskAsync(task)
	return next, nil
}

func (c Client) recordAcceptedTask(task protocol.SyncTask) error {
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, error_message, updated_at)
		VALUES (?, ?, ?, 'running', NULL, ?)
		ON CONFLICT(task_id) DO UPDATE SET asset_id = excluded.asset_id,
		task_type = excluded.task_type, state = 'running',
		error_message = NULL, updated_at = excluded.updated_at`,
		task.TaskID, task.Asset.AssetID, task.TaskType,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (c Client) revertAcceptedTask(taskID string) error {
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`DELETE FROM local_sync_tasks WHERE task_id = ? AND state = 'running'`, taskID)
	return err
}

func (c Client) sendTaskAck(conn net.Conn, reqID string, sequence uint64,
	task protocol.SyncTask) (uint64, error) {
	slots := c.availableSyncTaskSlots()
	body, _ := json.Marshal(protocol.SyncTaskAck{
		TaskID: task.TaskID, State: "running",
		SyncTaskSlotsAvailable: &slots,
	})
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "node sync task ack sent",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID))
	}
	messageID := reqID + "-" + task.TaskID + "-task-ack"
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: messageID,
		MessageType: protocol.TypeSyncTaskAck, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedAck(conn, reqID, &next, protocol.TypeHeartbeatAck, messageID)
	return next, err
}

func (c Client) sendTaskResult(conn net.Conn, reqID string, sequence uint64,
	result protocol.SyncTaskResult) (uint64, error) {
	slots := c.availableSyncTaskSlots()
	result.SyncTaskSlotsAvailable = &slots
	result.Message = trimSyncTaskResultMessage(result.Message)
	body, _ := json.Marshal(result)
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "node sync task result sent",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", result.TaskID),
			slog.String("asset_id", result.AssetID),
			slog.String("result", result.Result))
	}
	messageID := reqID + "-task-result"
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: messageID,
		MessageType: protocol.TypeSyncTaskResult, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedAck(conn, reqID, &next, protocol.TypeHeartbeatAck, messageID)
	return next, err
}
