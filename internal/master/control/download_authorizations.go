package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const maxAuthorizationDispatchPerWake = 20

func (s ControlServer) dispatchDownloadAuthorizations(conn net.Conn,
	session Session, reqID string) (int, error) {
	dispatched := 0
	for dispatched < maxAuthorizationDispatchPerWake {
		auth, ok, err := s.Repo.NextDownloadAuthorization(context.Background(), session.NodeID)
		if err != nil || !ok {
			return dispatched, err
		}
		body, _ := json.Marshal(auth)
		if err := writeControlFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version, MessageID: auth.AuthorizationID,
			MessageType: protocol.TypeDownloadAuthorization, SentAt: time.Now().UTC(),
			NodeID: session.NodeID, RequestID: reqID, Payload: body,
		}); err != nil {
			return dispatched, err
		}
		msg, err := readControlFrame(conn, s.sessionReadTimeout())
		if err != nil {
			return dispatched, err
		}
		if msg.NodeID != session.NodeID {
			return dispatched, fmt.Errorf("节点标识不匹配")
		}
		if err := msg.Validate(protocol.Control); err != nil {
			return dispatched, err
		}
		if msg.MessageType != protocol.TypeDownloadAuthorizationAck {
			result, err := s.handleMessage(session, msg)
			if err != nil {
				return dispatched, err
			}
			if err := s.writeMessageAck(conn, session, reqID, msg, result.HeartbeatResult); err != nil {
				return dispatched, err
			}
			continue
		}
		var ack protocol.DownloadAuthorizationAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			return dispatched, err
		}
		result, err := s.Repo.AcceptDownloadAuthorizationAck(context.Background(),
			session, msg.Sequence, ack)
		if err != nil {
			return dispatched, err
		}
		if err := s.writeMessageAck(conn, session, reqID, msg, result); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	return dispatched, nil
}

func (r Repository) NextDownloadAuthorization(ctx context.Context,
	nodeID string) (protocol.DownloadAuthorization, bool, error) {
	var out protocol.DownloadAuthorization
	var issued, expires string
	err := r.DB.QueryRowContext(ctx, `SELECT id, token_hash, asset_id, node_id,
		client_prefix_key, issued_at, expires_at, first_connection_timeout_seconds,
		idle_timeout_seconds, max_duration_seconds, max_bytes, traffic_limit_bytes,
		range_limit, request_id FROM download_authorizations
		WHERE node_id = ? AND token_hash != '' AND delivered_at = ''
		AND status IN ('issued', 'active')
		ORDER BY issued_at LIMIT 1`, nodeID).
		Scan(&out.AuthorizationID, &out.TokenHash, &out.AssetID, &out.NodeID,
			&out.ClientPrefix, &issued, &expires, &out.FirstConnectionSeconds,
			&out.IdleTimeoutSeconds, &out.MaxDurationSeconds, &out.MaxBytes,
			&out.TrafficLimitBytes, &out.RangeConcurrencyLimit, &out.RequestID)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.IssuedAt, _ = time.Parse(time.RFC3339Nano, issued)
	out.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	return out, true, nil
}

func (r Repository) AcceptDownloadAuthorizationAck(ctx context.Context, session Session,
	seq uint64, ack protocol.DownloadAuthorizationAck) (HeartbeatResult, error) {
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
	if ack.AuthorizationID == "" {
		return HeartbeatResult{}, fmt.Errorf("下载授权 ACK 缺少授权标识")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `UPDATE download_authorizations
		SET delivered_at = CASE WHEN delivered_at = '' THEN ? ELSE delivered_at END
		WHERE id = ? AND node_id = ?`,
		now, ack.AuthorizationID, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return HeartbeatResult{}, err
	}
	if changed == 0 {
		return HeartbeatResult{}, fmt.Errorf("下载授权 ACK 无效：授权不存在")
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	ready := routingReady(ctx, tx, session.NodeID)
	return HeartbeatResult{AcceptedSequence: seq,
		ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
}
