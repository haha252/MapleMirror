package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) sendPressureReport(conn net.Conn, reqID string, sequence uint64) error {
	active := c.activeDownloads()
	body, _ := json.Marshal(protocol.PressureReport{
		ReportID:            reqID + "-pressure",
		SampledAt:           time.Now().UTC(),
		SampleWindowSeconds: int64(c.heartbeatWindowSeconds()),
		TargetBandwidthBPS:  c.TargetBandwidthBPS,
		ActualBandwidthBPS:  0,
		PressureRatio:       pressureRatio(0, c.TargetBandwidthBPS),
		ActiveDownloads:     active,
		FreeBytes:           0,
	})
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点发送压力报告",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.Uint64("sequence", sequence),
			slog.Int64("target_bandwidth_bps", c.TargetBandwidthBPS),
			slog.Int64("active_downloads", active))
	}
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID + "-pressure",
		MessageType: protocol.TypePressureReport, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return err
	}
	_, err := c.readExpectedResponse(conn, reqID, protocol.TypeHeartbeatAck)
	return err
}

func (c Client) activeDownloads() int64 {
	return c.TaskLimiter.Active()
}

func (c Client) heartbeatWindowSeconds() int {
	if c.HeartbeatInterval > 0 {
		seconds := int(c.HeartbeatInterval.Seconds())
		if seconds > 0 {
			return seconds
		}
	}
	return 1
}

func pressureRatio(actual, target int64) float64 {
	if target <= 0 {
		return 0
	}
	return float64(actual) / float64(target)
}
