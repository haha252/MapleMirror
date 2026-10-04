package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) sendPressureReport(conn net.Conn, reqID string, sequence uint64,
	actualBandwidth int64) (uint64, error) {
	active := c.activeDownloads()
	slots := c.availableSyncTaskSlots()
	body, _ := json.Marshal(protocol.PressureReport{
		MirrorTraffic:          c.Activity.SampleTraffic(),
		DownloadPressure:       c.sampleDownloadPressure(actualBandwidth),
		ReportID:               reqID + "-pressure",
		SampledAt:              time.Now().UTC(),
		SampleWindowSeconds:    int64(c.heartbeatWindowSeconds()),
		TargetBandwidthBPS:     c.TargetBandwidthBPS,
		ActualBandwidthBPS:     actualBandwidth,
		PressureRatio:          pressureRatio(actualBandwidth, c.TargetBandwidthBPS),
		ActiveDownloads:        active,
		FreeBytes:              c.legacyFreeBytes(),
		MaxMirrorProjects:      c.MaxMirrorProjects,
		SyncTaskSlotsAvailable: &slots,
	})
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点发送压力报告",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID),
			slog.Uint64("sequence", sequence),
			slog.Int64("target_bandwidth_bps", c.TargetBandwidthBPS),
			slog.Int64("actual_bandwidth_bps", actualBandwidth),
			slog.Int64("active_downloads", active))
	}
	messageID := reqID + "-pressure"
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: messageID,
		MessageType: protocol.TypePressureReport, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedAck(conn, reqID, &next, protocol.TypeHeartbeatAck, messageID)
	if err != nil {
		return sequence, err
	}
	if c.Logger != nil {
		c.Logger.Debug(context.Background(), "节点压力报告 ack received",
			slog.String("node_id", c.NodeID),
			slog.String("request_id", reqID))
	}
	return next, nil
}

func (c Client) activeDownloads() int64 {
	if c.Activity != nil {
		return c.Activity.PublicDownloads()
	}
	if c.TaskLimiter == nil {
		return 0
	}
	return c.TaskLimiter.Active()
}

func (c Client) legacyFreeBytes() int64 {
	if c.Capacity == nil {
		return 0
	}
	snapshot := c.Capacity.Snapshot()
	if !snapshot.Asset.Valid {
		return 0
	}
	available := snapshot.Asset.AvailableBytes - snapshot.ReservedAsset
	if available < 0 {
		return 0
	}
	return available
}

func (c Client) sampleBandwidth() int64 {
	if c.Bandwidth == nil {
		return 0
	}
	return c.Bandwidth.SampleBandwidthBPS(c.HeartbeatInterval)
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

func (c Client) sampleDownloadPressure(actual int64) *protocol.DownloadPressure {
	if c.Activity == nil {
		return nil
	}
	return c.Activity.Network.Sample(c.TargetBandwidthBPS, actual)
}
