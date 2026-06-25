package control

import (
	"context"
	"database/sql"
	"fmt"

	"mirror-server/internal/protocol"
)

func existingLegacyTraffic(ctx context.Context, tx *sql.Tx, nodeID string, event protocol.TrafficEvent) (bool, error) {
	var bytes int64
	var authID, assetID, nodeReqID, masterReqID, status string
	err := tx.QueryRowContext(ctx, `SELECT authorization_id, asset_id, node_request_id,
		master_request_id, sent_bytes, status FROM traffic_events
		WHERE node_id = ? AND event_sequence = ?`, nodeID, event.EventSequence).
		Scan(&authID, &assetID, &nodeReqID, &masterReqID, &bytes, &status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if authID != event.AuthorizationID || assetID != event.AssetID ||
		nodeReqID != event.NodeRequestID || masterReqID != event.MasterRequestID ||
		bytes != event.SentBytes || status != event.Status {
		return true, fmt.Errorf("重复流量事件内容不一致")
	}
	return true, nil
}
