package control

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/master/accountingarchive"
	"mirror-server/internal/protocol"
)

func (r Repository) archiveTrafficEvent(ctx context.Context, nodeID string,
	event protocol.TrafficEvent, accountedAt string, committed time.Time) {
	if r.Archive == nil {
		return
	}
	err := r.Archive.WriteTraffic(ctx, accountingarchive.TrafficRecord{
		NodeID:          nodeID,
		EventSequence:   event.EventSequence,
		AuthorizationID: event.AuthorizationID,
		AssetID:         event.AssetID,
		NodeRequestID:   event.NodeRequestID,
		MasterRequestID: event.MasterRequestID,
		SentBytes:       event.SentBytes,
		Status:          event.Status,
		ReportedAt:      event.ReportedAt,
		AccountedAt:     accountedAt,
	}, committed)
	if err != nil && r.Logger != nil {
		r.Logger.Warn(ctx, "流量事件归档写入失败",
			slog.String("authorization_id", event.AuthorizationID),
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
	}
}

func (r Repository) archiveAuthorizationStatus(ctx context.Context, nodeID string,
	event protocol.AuthorizationStatusEvent, committed time.Time) {
	if r.Archive == nil {
		return
	}
	err := r.Archive.WriteAuthorizationStatus(ctx, accountingarchive.AuthorizationStatusRecord{
		NodeID:          nodeID,
		AuthorizationID: event.AuthorizationID,
		AssetID:         event.AssetID,
		Status:          event.Status,
		Reason:          event.Reason,
		OccurredAt:      event.OccurredAt,
	}, committed)
	if err != nil && r.Logger != nil {
		r.Logger.Warn(ctx, "授权状态归档写入失败",
			slog.String("authorization_id", event.AuthorizationID),
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()))
	}
}

func parseArchiveTime(value string) time.Time {
	when, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().UTC()
	}
	return when.UTC()
}
