package control

import (
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) handleDownloadAuthorization(conn net.Conn, reqID string,
	sequence uint64, msg protocol.Envelope) (uint64, error) {
	var auth protocol.DownloadAuthorization
	if err := json.Unmarshal(msg.Payload, &auth); err != nil {
		return sequence, err
	}
	if err := c.storeDownloadAuthorization(auth); err != nil {
		return sequence, err
	}
	body, _ := json.Marshal(protocol.DownloadAuthorizationAck{
		AcceptedSequence: sequence,
		AuthorizationID:  auth.AuthorizationID,
		Message:          "下载授权已缓存",
	})
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       msg.MessageID + "-ack",
		MessageType:     protocol.TypeDownloadAuthorizationAck,
		SentAt:          time.Now().UTC(),
		NodeID:          c.NodeID,
		RequestID:       reqID,
		Sequence:        sequence,
		ReplyTo:         msg.MessageID,
		Payload:         body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedAck(conn, reqID, &next,
		protocol.TypeHeartbeatAck, msg.MessageID+"-ack")
	return next, err
}

func (c Client) storeDownloadAuthorization(auth protocol.DownloadAuthorization) error {
	if c.DB == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := c.DB.ExecContext(c.controlContext(), `INSERT INTO local_authorizations
		(authorization_id, token_hash, asset_id, node_id, client_prefix_key,
		 issued_at, expires_at, first_seen_at, last_activity_at, status, reason,
		 reported_at, created_at, updated_at, max_bytes, traffic_limit_bytes,
		 range_limit, request_id, first_connection_timeout_seconds,
		 idle_timeout_seconds, max_duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, 'issued', '', NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(authorization_id) DO UPDATE SET
			token_hash = excluded.token_hash,
			asset_id = excluded.asset_id,
			node_id = excluded.node_id,
			client_prefix_key = excluded.client_prefix_key,
			issued_at = excluded.issued_at,
			expires_at = excluded.expires_at,
			max_bytes = excluded.max_bytes,
			traffic_limit_bytes = excluded.traffic_limit_bytes,
			range_limit = excluded.range_limit,
			request_id = excluded.request_id,
			first_connection_timeout_seconds = excluded.first_connection_timeout_seconds,
			idle_timeout_seconds = excluded.idle_timeout_seconds,
			max_duration_seconds = excluded.max_duration_seconds,
			updated_at = excluded.updated_at`,
		auth.AuthorizationID, auth.TokenHash, auth.AssetID, auth.NodeID,
		auth.ClientPrefix, auth.IssuedAt.Format(time.RFC3339Nano),
		auth.ExpiresAt.Format(time.RFC3339Nano), now, now,
		auth.MaxBytes, auth.TrafficLimitBytes, auth.RangeConcurrencyLimit,
		auth.RequestID, auth.FirstConnectionSeconds, auth.IdleTimeoutSeconds,
		auth.MaxDurationSeconds)
	return err
}
