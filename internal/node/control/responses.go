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

func (c Client) readExpectedResponse(conn net.Conn, reqID string, expected ...string) (protocol.Envelope, error) {
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
			if err := c.handleSyncTask(msg, reqID); err != nil {
				return protocol.Envelope{}, err
			}
			continue
		}
		for _, messageType := range expected {
			if msg.MessageType == messageType {
				return msg, nil
			}
		}
		return protocol.Envelope{}, errors.New("涓昏妭鐐硅繑鍥炰簡闈為鏈熺殑鎺у埗鍝嶅簲")
	}
}

func (c Client) handleSyncTask(msg protocol.Envelope, reqID string) error {
	var task protocol.SyncTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		return err
	}
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "鑺傜偣鏀跺埌鍚屾浠诲姟",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("file_name", task.Asset.FileName))
	}
	if c.Executor == nil {
		if c.Logger != nil {
			c.Logger.Debug(context.Background(), "鑺傜偣鍚屾鎵ц鍣ㄦ湭鍚敤",
				slog.String("node_id", c.NodeID),
				slog.String("request_id", reqID),
				slog.String("task_id", task.TaskID))
		}
		return c.storePendingTaskResult(protocol.SyncTaskResult{
			TaskID: task.TaskID, AssetID: task.Asset.AssetID,
			Result: "temporary_error", Message: "鑺傜偣鍚屾鎵ц鍣ㄦ湭鍚敤",
		})
	}
	c.executeTaskAsync(task)
	return nil
}
