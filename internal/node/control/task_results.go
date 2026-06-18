package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

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
		sequence = next
	}
	if len(results) > 0 {
		return c.readReadyOptionalTasksToCapacity(conn, reqID, sequence)
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
		sequence = next
	}
	if len(items) > 0 {
		return c.readReadyOptionalTasksToCapacity(conn, reqID, sequence)
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
	var peerFallbackAttempted int
	err := rows.Scan(&result.TaskID, &result.AssetID, &result.Result,
		&result.LocalDigestSHA256, &result.SizeBytes, &result.Message,
		&peerFallbackAttempted)
	result.PeerFallbackAttempted = peerFallbackAttempted != 0
	return result, err
}

func (c *Client) executeTaskAsync(task protocol.SyncTask) {
	go func() {
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
		c.releaseSyncTaskSlot()
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
