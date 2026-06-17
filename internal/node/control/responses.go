package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) writeFrame(conn net.Conn, envelope protocol.Envelope) error {
	_ = conn.SetWriteDeadline(time.Now().Add(controlIOTimeout))
	err := protocol.WriteFrame(conn, envelope)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func (c Client) readExpectedResponse(conn net.Conn, reqID string,
	sequence *uint64, expected ...string) (protocol.Envelope, error) {
	deadline := time.Now().Add(controlIOTimeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			return protocol.Envelope{}, err
		}
		if msg.MessageType == protocol.TypeProtocolError {
			return protocol.Envelope{}, parseRejectionError(msg)
		}
		if msg.MessageType == protocol.TypeSyncTask {
			if sequence == nil {
				return protocol.Envelope{}, errors.New("收到同步任务但当前控制序号不可用")
			}
			next, err := c.handleDispatchedTask(conn, reqID, *sequence, msg)
			if err != nil {
				return protocol.Envelope{}, err
			}
			*sequence = next
			continue
		}
		for _, messageType := range expected {
			if msg.MessageType == messageType {
				return msg, nil
			}
		}
		return protocol.Envelope{}, errors.New("主节点返回了非预期的控制响应")
	}
}

func (c Client) decodeSyncTask(msg protocol.Envelope, reqID string) (protocol.SyncTask, error) {
	var task protocol.SyncTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		return protocol.SyncTask{}, err
	}
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点收到同步任务",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("file_name", task.Asset.FileName),
			slog.Int("fallback_sources", len(task.FallbackSources)))
	}
	return task, nil
}

func (c Client) logSyncExecutorDisabled(reqID, taskID string) {
	if c.Logger == nil {
		return
	}
	c.Logger.Debug(context.Background(), "节点同步执行器未启用",
		slog.String("node_id", c.NodeID),
		slog.String("request_id", reqID),
		slog.String("task_id", taskID))
}

func disabledExecutorResult(task protocol.SyncTask) protocol.SyncTaskResult {
	return protocol.SyncTaskResult{
		TaskID: task.TaskID, AssetID: task.Asset.AssetID,
		Result: "temporary_error", Message: "节点同步执行器未启用",
	}
}
