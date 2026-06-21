package control

import (
	"context"
	"database/sql"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"mirror-server/internal/protocol"
)

func (r Repository) AcceptTrafficEvent(ctx context.Context, session Session, seq uint64, event protocol.TrafficEvent) (HeartbeatResult, error) {
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
		dup, err := existingTraffic(ctx, tx, session.NodeID, event)
		if err != nil {
			return HeartbeatResult{}, err
		}
		if !dup {
			return HeartbeatResult{}, fmt.Errorf("流量事件序号已确认但事件不存在")
		}
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	if event.SentBytes < 0 {
		return HeartbeatResult{}, fmt.Errorf("流量字节数不合法")
	}
	dup, err := existingTraffic(ctx, tx, session.NodeID, event)
	if err != nil || dup {
		if err == nil {
			err = r.updateSequence(session, seq)
		}
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, finish(tx, err)
	}
	info, err := loadAuthorization(ctx, tx, session.NodeID, event)
	if err != nil {
		return HeartbeatResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO traffic_events
		(node_id, event_sequence, authorization_id, node_request_id,
		master_request_id, sent_bytes, reported_at, accounted_at, asset_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.NodeID, event.EventSequence, event.AuthorizationID,
		event.NodeRequestID, event.MasterRequestID, event.SentBytes,
		event.ReportedAt.Format(time.RFC3339Nano), now, event.AssetID, event.Status)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if event.SentBytes > 0 && !info.Started {
		_, err = tx.ExecContext(ctx, `UPDATE download_authorizations
			SET first_transfer_at = ?, status = CASE WHEN status = 'issued' THEN 'active' ELSE status END,
				status_updated_at = CASE WHEN status = 'issued' THEN ? ELSE status_updated_at END
			WHERE id = ? AND first_transfer_at IS NULL`,
			now, now, event.AuthorizationID)
		if err != nil {
			return HeartbeatResult{}, err
		}
		info.StartedIncrement = 1
	}
	if err := updateTrafficStats(ctx, tx, info, event.SentBytes, now); err != nil {
		return HeartbeatResult{}, err
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	ready := routingReady(ctx, tx, session.NodeID)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
}

type authAccounting struct {
	AuthorizationID  string
	AssetID          string
	NodeID           string
	ProjectID        string
	Day              string
	AddressKind      string
	AddressKey       string
	NetworkKind      string
	NetworkKey       string
	Started          bool
	StartedIncrement int64
}

func existingTraffic(ctx context.Context, tx *sql.Tx, nodeID string, event protocol.TrafficEvent) (bool, error) {
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

func loadAuthorization(ctx context.Context, tx *sql.Tx, nodeID string, event protocol.TrafficEvent) (authAccounting, error) {
	var info authAccounting
	info.AuthorizationID = event.AuthorizationID
	info.AssetID = event.AssetID
	info.NodeID = nodeID
	var first sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT r.project_id, tr.scope_day,
		tr.address_scope_kind, tr.address_scope_key,
		tr.network_scope_kind, tr.network_scope_key, da.first_transfer_at
		FROM download_authorizations da
		JOIN assets a ON a.id = da.asset_id
		JOIN releases r ON r.id = a.release_id
		JOIN traffic_reservations tr ON tr.authorization_id = da.id
		WHERE da.id = ? AND da.node_id = ? AND da.asset_id = ?
		AND da.request_id = ?`,
		event.AuthorizationID, nodeID, event.AssetID, event.MasterRequestID).
		Scan(&info.ProjectID, &info.Day, &info.AddressKind, &info.AddressKey,
			&info.NetworkKind, &info.NetworkKey, &first)
	if err != nil {
		if err == sql.ErrNoRows {
			return loadLegacyAuthorization(ctx, tx, nodeID, event)
		}
		return info, err
	}
	info.Started = first.Valid && first.String != ""
	return info, nil
}

func loadLegacyAuthorization(ctx context.Context, tx *sql.Tx, nodeID string, event protocol.TrafficEvent) (authAccounting, error) {
	var info authAccounting
	info.AuthorizationID = event.AuthorizationID
	info.AssetID = event.AssetID
	info.NodeID = nodeID
	var first sql.NullString
	var prefix string
	var maxBytes int64
	err := tx.QueryRowContext(ctx, `SELECT r.project_id, da.client_prefix_key,
		da.max_bytes, da.first_transfer_at FROM download_authorizations da
		JOIN assets a ON a.id = da.asset_id JOIN releases r ON r.id = a.release_id
		WHERE da.id = ? AND da.node_id = ? AND da.asset_id = ? AND da.request_id = ?`,
		event.AuthorizationID, nodeID, event.AssetID, event.MasterRequestID).
		Scan(&info.ProjectID, &prefix, &maxBytes, &first)
	if err != nil {
		return info, err
	}
	scopes, err := trafficScopes(prefix)
	if err != nil {
		return info, err
	}
	now := time.Now().UTC()
	info.Day = now.In(time.Local).Format("2006-01-02")
	info.AddressKind, info.AddressKey = scopes[0].Kind, scopes[0].Key
	info.NetworkKind, info.NetworkKey = scopes[1].Kind, scopes[1].Key
	info.Started = first.Valid && first.String != ""
	_, err = tx.ExecContext(ctx, `INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES (?, ?, ?, ?, 0, 'legacy_m4', ?, ?, ?, ?, ?)`,
		event.AuthorizationID, info.Day, maxBytes, maxBytes, now.Format(time.RFC3339Nano),
		info.AddressKind, info.AddressKey, info.NetworkKind, info.NetworkKey)
	return info, err
}

type trafficScope struct {
	Kind string
	Key  string
}

func trafficScopes(prefix string) ([2]trafficScope, error) {
	host := strings.TrimSuffix(strings.TrimSuffix(prefix, "/32"), "/128")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return [2]trafficScope{}, err
	}
	if addr.Is4() {
		return [2]trafficScope{
			{"ipv4_32", addr.String() + "/32"},
			{"ipv4_24", netip.PrefixFrom(addr, 24).Masked().String()},
		}, nil
	}
	return [2]trafficScope{
		{"ipv6_128", addr.String() + "/128"},
		{"ipv6_64", netip.PrefixFrom(addr, 64).Masked().String()},
	}, nil
}

func updateTrafficStats(ctx context.Context, tx *sql.Tx, info authAccounting, bytes int64, now string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE traffic_reservations
		SET settled_bytes = settled_bytes + ? WHERE authorization_id = ? AND scope_day = ?
		AND address_scope_kind = ? AND address_scope_key = ?
		AND network_scope_kind = ? AND network_scope_key = ?`,
		bytes, info.AuthorizationID, info.Day, info.AddressKind, info.AddressKey, info.NetworkKind, info.NetworkKey); err != nil {
		return err
	}
	if err := upsertTrafficDay(ctx, tx, info.Day, info.AddressKind, info.AddressKey, bytes, now); err != nil {
		return err
	}
	if err := upsertTrafficDay(ctx, tx, info.Day, info.NetworkKind, info.NetworkKey, bytes, now); err != nil {
		return err
	}
	if err := upsertProjectTraffic(ctx, tx, info, bytes); err != nil {
		return err
	}
	if err := upsertAssetStats(ctx, tx, info.Day, info.AssetID, 0,
		info.StartedIncrement, bytes, now); err != nil {
		return err
	}
	return upsertNodeTraffic(ctx, tx, info.Day, info.NodeID, bytes, now)
}
