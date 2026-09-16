package control

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mirror-server/internal/master/accountingstate"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func legacyTrafficEvent(event protocolv2.TrafficEvent) protocol.TrafficEvent {
	return protocol.TrafficEvent{
		EventSequence: event.EventSequence, AuthorizationID: event.AuthorizationID,
		AssetID: event.AssetID, NodeRequestID: event.NodeRequestID,
		MasterRequestID: event.MasterRequestID, SentBytes: event.SentBytes,
		Status: event.Status, ReportedAt: event.ReportedAt,
	}
}

func (r Repository) AcceptV2TrafficEvent(ctx context.Context, session Session, event protocolv2.TrafficEvent) (bool, error) {
	if event.EventSequence == 0 || event.SentBytes < 0 || event.AuthorizationID == "" {
		return false, fmt.Errorf("invalid traffic event")
	}
	legacy := legacyTrafficEvent(event)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	dup, err := existingTraffic(ctx, tx, session.NodeID, legacy)
	if err != nil {
		return false, err
	}
	if dup {
		return true, tx.Commit()
	}
	info, err := loadAuthorization(ctx, tx, session.NodeID, legacy)
	if err == sql.ErrNoRows {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := updateTrafficCursor(ctx, tx, session.NodeID, event.EventSequence, now); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO traffic_event_dedupe
		(node_id,event_sequence,authorization_id,event_hash,accounted_at) VALUES(?,?,?,?,?)`,
		session.NodeID, event.EventSequence, event.AuthorizationID,
		accountingstate.TrafficEventHash(session.NodeID, legacy), now)
	if err != nil {
		return false, err
	}
	if err := pruneTrafficDedupeWindow(ctx, tx, session.NodeID, event.EventSequence); err != nil {
		return false, err
	}
	archiveStarted := event.SentBytes > 0 && !info.Started && info.Status == "issued"
	if event.SentBytes > 0 && !info.Started {
		if _, err = tx.ExecContext(ctx, `UPDATE download_authorizations
			SET first_transfer_at=?, status=CASE WHEN status='issued' THEN 'active' ELSE status END,
			status_updated_at=CASE WHEN status='issued' THEN ? ELSE status_updated_at END
			WHERE id=? AND first_transfer_at IS NULL`, now, now, event.AuthorizationID); err != nil {
			return false, err
		}
		info.StartedIncrement = 1
	}
	if err := updateDownloadHistoryTraffic(ctx, tx, event.AuthorizationID, event.SentBytes, now); err != nil {
		return false, err
	}
	if err := updateTrafficStats(ctx, tx, info, event.SentBytes, now, r.StatsBuffer == nil); err != nil {
		return false, err
	}
	if err := updateTrafficCursor(ctx, tx, session.NodeID, event.EventSequence, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if r.StatsBuffer != nil {
		r.bufferTrafficStats(info, event.SentBytes)
	}
	r.archiveTrafficEvent(ctx, session.NodeID, legacy, now, time.Now().UTC())
	if archiveStarted {
		r.archiveAuthorizationStatus(ctx, session.NodeID, protocol.AuthorizationStatusEvent{
			AuthorizationID: event.AuthorizationID, AssetID: event.AssetID, Status: "active", OccurredAt: parseArchiveTime(now),
		}, time.Now().UTC())
	}
	return false, nil
}

func (r Repository) AcceptV2AuthorizationStatus(ctx context.Context, session Session, event protocolv2.AuthorizationStatus) error {
	if !validAuthorizationStatus(event.Status) || event.AuthorizationID == "" {
		return fmt.Errorf("invalid authorization status")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var nodeID, assetID string
	if err := tx.QueryRowContext(ctx, `SELECT node_id,asset_id FROM download_authorizations WHERE id=?`, event.AuthorizationID).Scan(&nodeID, &assetID); err != nil {
		return err
	}
	if nodeID != session.NodeID || assetID != event.AssetID {
		return fmt.Errorf("authorization node/asset mismatch")
	}
	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	_, err = tx.ExecContext(ctx, `UPDATE download_authorizations SET status=?,status_reason=?,status_updated_at=? WHERE id=?`,
		event.Status, event.Reason, occurred.Format(time.RFC3339Nano), event.AuthorizationID)
	if err != nil {
		return err
	}
	if err := updateDownloadHistoryStatus(ctx, tx, event.AuthorizationID, event.Status, event.Reason, occurred.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.archiveAuthorizationStatus(ctx, session.NodeID, protocol.AuthorizationStatusEvent{
		AuthorizationID: event.AuthorizationID, AssetID: event.AssetID, Status: event.Status, Reason: event.Reason, OccurredAt: occurred,
	}, time.Now().UTC())
	return nil
}

func (r Repository) AcceptV2DownloadAuthorizationAck(ctx context.Context, session Session, authorizationID string) error {
	if authorizationID == "" {
		return fmt.Errorf("authorization id required")
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE download_authorizations SET delivered_at=CASE WHEN delivered_at='' THEN ? ELSE delivered_at END WHERE id=? AND node_id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), authorizationID, session.NodeID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("authorization not found")
	}
	return nil
}
