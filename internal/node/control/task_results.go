package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"time"

	"mirror-server/internal/protocol"
)

const pendingTaskResultStoreAttempts = 5

func (c *Client) sendPendingTaskResults(conn net.Conn, reqID string, sequence uint64) (uint64, error) {
	if c.DB == nil {
		return sequence, nil
	}
	results, err := c.loadPendingTaskResults(20)
	if err != nil {
		return sequence, err
	}
	for _, result := range results {
		next, err := c.sendTaskResult(conn, reqID, sequence, result)
		if err != nil {
			return sequence, err
		}
		_, err = c.DB.Exec(`UPDATE pending_sync_task_results SET reported_at = ?
			WHERE task_id = ?`, time.Now().UTC().Format(time.RFC3339Nano), result.TaskID)
		if err != nil {
			return sequence, err
		}
		next, err = c.readOptionalTasksToCapacity(conn, reqID, next)
		if err != nil {
			return sequence, err
		}
		sequence = next
	}
	return sequence, nil
}

func (c *Client) sendRunningTaskAcks(conn net.Conn, reqID string, sequence uint64) (uint64, error) {
	if c.DB == nil {
		return sequence, nil
	}
	items, err := c.loadRunningTaskAcks(50)
	if err != nil {
		return sequence, err
	}
	for _, ack := range items {
		next, err := c.sendRunningTaskAck(conn, reqID, sequence, ack)
		if err != nil {
			return sequence, err
		}
		next, err = c.readOptionalTasksToCapacity(conn, reqID, next)
		if err != nil {
			return sequence, err
		}
		sequence = next
	}
	return sequence, nil
}

func (c *Client) sendRunningTaskAck(conn net.Conn, reqID string, sequence uint64, ack protocol.SyncTaskAck) (uint64, error) {
	slots := c.availableSyncTaskSlots()
	ack.SyncTaskSlotsAvailable = &slots
	body, _ := json.Marshal(ack)
	if c.shouldLogRunningTaskAck(ack.TaskID) && c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点续报运行中的同步任务",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", ack.TaskID))
	}
	messageID := reqID + "-" + ack.TaskID + "-running"
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

func (c *Client) shouldLogRunningTaskAck(taskID string) bool {
	if c.runningTaskAckLogged == nil {
		c.runningTaskAckLogged = map[string]time.Time{}
	}
	now := time.Now()
	last, ok := c.runningTaskAckLogged[taskID]
	if ok && now.Sub(last) < runningTaskAckLogInterval {
		return false
	}
	c.runningTaskAckLogged[taskID] = now
	return true
}

func scanPendingTaskResult(rows *sql.Rows) (protocol.SyncTaskResult, error) {
	var result protocol.SyncTaskResult
	err := rows.Scan(&result.TaskID, &result.AssetID, &result.Result,
		&result.LocalDigestSHA256, &result.SizeBytes, &result.Message)
	return result, err
}

func (c *Client) executeTaskAsync(task protocol.SyncTask) {
	go func() {
		defer c.releaseSyncTaskSlot()
		ctx, cancel := context.WithTimeout(context.Background(), c.syncTaskTimeout())
		defer cancel()
		var result protocol.SyncTaskResult
		err := c.TaskLimiter.Run(ctx, func() {
			result = c.Executor.Execute(ctx, task)
		})
		if err != nil {
			result = protocol.SyncTaskResult{
				TaskID: task.TaskID, AssetID: task.Asset.AssetID,
				Result: "temporary_error", Message: "同步任务等待或执行超时",
			}
		}
		if result.Result == "" && ctx.Err() != nil {
			result = protocol.SyncTaskResult{
				TaskID: task.TaskID, AssetID: task.Asset.AssetID,
				Result: "temporary_error", Message: "同步任务执行超时",
			}
		}
		if c.Logger != nil {
			c.Logger.Debug(context.Background(), "节点完成同步任务执行",
				slog.String("node_id", c.NodeID),
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("result", result.Result),
				slog.Int64("size_bytes", result.SizeBytes))
		}
		if err := c.storePendingTaskResultWithRetry(result); err != nil {
			if c.Logger != nil {
				c.Logger.Warn(context.Background(), "节点保存待上报同步结果失败",
					slog.String("node_id", c.NodeID),
					slog.String("task_id", result.TaskID),
					slog.String("asset_id", result.AssetID),
					slog.String("error", err.Error()))
			}
			return
		}
		c.wakeControlWork()
	}()
}

func (c *Client) storePendingTaskResultWithRetry(result protocol.SyncTaskResult) error {
	var err error
	for attempt := 1; attempt <= pendingTaskResultStoreAttempts; attempt++ {
		err = c.storePendingTaskResult(result)
		if err == nil {
			return nil
		}
		if !retryableSQLiteError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt*25) * time.Millisecond)
	}
	return err
}

func retryableSQLiteError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "busy") ||
		strings.Contains(message, "locked")
}

func (c *Client) storePendingTaskResult(result protocol.SyncTaskResult) error {
	if c.DB == nil {
		return nil
	}
	tx, err := c.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(task_id) DO UPDATE SET asset_id = excluded.asset_id,
		result = excluded.result, local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, message = excluded.message,
		created_at = excluded.created_at, reported_at = NULL`,
		result.TaskID, result.AssetID, result.Result, nullableString(result.LocalDigestSHA256),
		result.SizeBytes, nullableString(result.Message), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if err := recordLocalTaskResult(tx, result); err != nil {
		return err
	}
	return tx.Commit()
}

func recordLocalTaskResult(tx *sql.Tx, result protocol.SyncTaskResult) error {
	state := result.Result
	switch result.Result {
	case "succeeded":
		state = "succeeded"
	case "temporary_error", "digest_mismatch", "size_mismatch":
		state = "failed"
	default:
		if state == "" || state == "running" {
			state = "failed"
		}
	}
	_, err := tx.Exec(`UPDATE local_sync_tasks SET state = ?,
		error_message = ?, updated_at = ? WHERE task_id = ?`,
		state, nullableString(result.Message), time.Now().UTC().Format(time.RFC3339Nano),
		result.TaskID)
	if err != nil {
		return err
	}
	if result.Result != "succeeded" {
		return nil
	}
	taskType, err := localTaskType(tx, result.TaskID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if taskType != "inventory_reconcile" {
		return nil
	}
	return forceInventoryReportDue(tx)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
