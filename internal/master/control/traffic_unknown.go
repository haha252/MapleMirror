package control

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) acceptUnknownTrafficEvent(ctx context.Context, tx *sql.Tx,
	session Session, seq uint64, event protocol.TrafficEvent, now time.Time) (HeartbeatResult, error) {
	nowText := now.Format(time.RFC3339Nano)
	if err := updateTrafficCursor(ctx, tx, session.NodeID, event.EventSequence, nowText); err != nil {
		return HeartbeatResult{}, err
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	ready := routingReady(ctx, tx, session.NodeID)
	if r.Logger != nil {
		r.Logger.Debug(ctx, "未知流量事件已忽略",
			slog.String("node_id", session.NodeID),
			slog.Uint64("event_sequence", event.EventSequence),
			slog.String("authorization_id", event.AuthorizationID),
			slog.String("asset_id", event.AssetID),
			slog.String("master_request_id", event.MasterRequestID))
	}
	return HeartbeatResult{
		AcceptedSequence: seq,
		ManagedState:     managedState(ready),
		RoutingReady:     ready,
	}, tx.Commit()
}
