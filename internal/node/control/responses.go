package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"mirror-server/internal/protocol"
)

const controlAckTimeout = 3 * time.Second

type serializedConn struct {
	net.Conn
	writeMu sync.Mutex
}

func (c *serializedConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.Conn.Write(p)
}

func (c *serializedConn) writeFrame(envelope protocol.Envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return protocol.WriteFrame(c.Conn, envelope)
}

func (c Client) writeFrame(conn net.Conn, envelope protocol.Envelope) error {
	_ = conn.SetWriteDeadline(time.Now().Add(controlIOTimeout))
	var err error
	if serialized, ok := conn.(*serializedConn); ok {
		err = serialized.writeFrame(envelope)
	} else {
		err = protocol.WriteFrame(conn, envelope)
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func (c Client) readFrame(conn net.Conn) (protocol.Envelope, error) {
	if c.frameReader != nil {
		return c.frameReader.ReadFrame(conn)
	}
	return protocol.ReadFrame(conn, protocol.MaxFrameBytes)
}

func (c Client) readExpectedResponse(conn net.Conn, reqID string,
	sequence *uint64, expected ...string) (protocol.Envelope, error) {
	deadline := time.Now().Add(controlAckTimeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		msg, err := c.readFrame(conn)
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
		if msg.MessageType == protocol.TypeDownloadAuthorization {
			if sequence == nil {
				return protocol.Envelope{}, errors.New("收到下载授权但当前控制序号不可用")
			}
			next, err := c.handleDownloadAuthorization(conn, reqID, *sequence, msg)
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

func (c Client) readExpectedAck(conn net.Conn, reqID string, sequence *uint64,
	expectedType, expectedReplyTo string) (protocol.Envelope, error) {
	sentSequence := uint64(0)
	if sequence != nil && *sequence > 0 {
		sentSequence = *sequence - 1
	}
	msg, err := c.readExpectedResponse(conn, reqID, sequence, expectedType)
	if err != nil {
		return protocol.Envelope{}, err
	}
	if expectedReplyTo != "" && msg.ReplyTo != expectedReplyTo {
		return protocol.Envelope{}, errors.New("主节点返回了错误的控制响应关联")
	}
	if err := validateAckSequence(msg, sentSequence); err != nil {
		return protocol.Envelope{}, err
	}
	return msg, nil
}

func validateAckSequence(msg protocol.Envelope, sentSequence uint64) error {
	if sentSequence == 0 {
		return nil
	}
	switch msg.MessageType {
	case protocol.TypeHeartbeatAck:
		var ack protocol.HeartbeatAckPayload
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			return err
		}
		if ack.AcceptedSequence < sentSequence {
			return errors.New("主节点 ACK 序号未覆盖当前控制消息")
		}
	case protocol.TypeTrafficEventAck:
		var ack protocol.TrafficEventAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			return err
		}
		if ack.AcceptedSequence < sentSequence {
			return errors.New("主节点流量 ACK 序号未覆盖当前控制消息")
		}
	case protocol.TypeAuthorizationStatusAck:
		var ack protocol.AuthorizationStatusAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			return err
		}
		if ack.AcceptedSequence < sentSequence {
			return errors.New("主节点授权状态 ACK 序号未覆盖当前控制消息")
		}
	}
	return nil
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
