package control

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) AcceptAuthorizationStatusEvent(ctx context.Context, session Session,
	seq uint64, event protocol.AuthorizationStatusEvent) (HeartbeatResult, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if seq <= last {
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last,
			ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	if !validAuthorizationStatus(event.Status) {
		return HeartbeatResult{}, fmt.Errorf("授权状态事件无效：%s", event.Status)
	}
	var nodeID, assetID string
	err = tx.QueryRowContext(ctx, `SELECT node_id, asset_id
		FROM download_authorizations WHERE id = ?`, event.AuthorizationID).
		Scan(&nodeID, &assetID)
	if err == sql.ErrNoRows {
		return HeartbeatResult{}, fmt.Errorf("授权状态事件无效：授权不存在")
	}
	if err != nil {
		return HeartbeatResult{}, err
	}
	if nodeID != session.NodeID || assetID != event.AssetID {
		return HeartbeatResult{}, fmt.Errorf("授权状态事件无效：节点或资产不匹配")
	}
	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	event.OccurredAt = occurred
	_, err = tx.ExecContext(ctx, `UPDATE download_authorizations
		SET status = ?, status_reason = ?, status_updated_at = ?
		WHERE id = ?`, event.Status, event.Reason,
		occurred.Format(time.RFC3339Nano), event.AuthorizationID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	ready := routingReady(ctx, tx, session.NodeID)
	result := HeartbeatResult{AcceptedSequence: seq,
		ManagedState: managedState(ready), RoutingReady: ready}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	r.archiveAuthorizationStatus(ctx, session.NodeID, event, time.Now().UTC())
	return result, nil
}

func validAuthorizationStatus(status string) bool {
	switch status {
	case "active", "expired_first_connection", "expired_idle", "expired_max_duration":
		return true
	default:
		return false
	}
}
