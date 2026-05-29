package public

import (
	"context"
	"database/sql"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
)

type AuthorizationDebug struct {
	ClientPrefix               string
	NodeID                     string
	ProjectID                  string
	ExpiresAt                  string
	MaxBytes                   int64
	RangeLimit                 int
	RequestRemainingMicrounits map[string]int64
	TrafficRemainingBytes      map[string]int64
}

func (s Store) IssueAuthorization(ctx context.Context, c Challenge, ttl time.Duration, reqID string) (IssuedAuthorization, AuthorizationDebug, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	defer tx.Rollback()
	nodeID, projectID, multiplier, size, err := s.routableAssetTx(ctx, tx, c.AssetID)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	now := time.Now().UTC()
	scopes, err := quotaScopes(c.ClientPrefixKey)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	quota := s.Quota
	if quota.buckets == nil {
		quota = defaultQuota()
	}
	if multiplier <= 0 {
		multiplier = 1
	}
	if err := quota.consume(ctx, tx, scopes, multiplier, now); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	day := statDay(now, s.Location)
	if err := quota.reserve(ctx, tx, day, scopes, size); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	authID, err := requestid.New()
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	expires := expiresAfter(ttl)
	if err := insertAuthorization(ctx, tx, authID, c, nodeID, size, expires, reqID); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	if err := insertReservation(ctx, tx, authID, day, size, now, scopes); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	if err := upsertProjectStats(ctx, tx, day, projectID, 1, 0, 0); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	if err := consumeChallenge(ctx, tx, c.ID); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	requestRemaining, trafficRemaining, err := quota.snapshot(ctx, tx, day, scopes)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version, AuthorizationID: authID,
		AssetID: c.AssetID, NodeID: nodeID, ClientPrefix: c.ClientPrefixKey,
		ExpiresAt: expires, MaxBytes: size, RangeConcurrencyLimit: 4, RequestID: reqID}
	debug := AuthorizationDebug{
		ClientPrefix:               c.ClientPrefixKey,
		NodeID:                     nodeID,
		ProjectID:                  projectID,
		ExpiresAt:                  expires,
		MaxBytes:                   size,
		RangeLimit:                 4,
		RequestRemainingMicrounits: requestRemaining,
		TrafficRemainingBytes:      trafficRemaining,
	}
	return IssuedAuthorization{Claims: claims}, debug, tx.Commit()
}

func (s Store) Authorization(ctx context.Context, id string) (AuthorizationStatus, error) {
	var out AuthorizationStatus
	err := s.DB.QueryRowContext(ctx, `SELECT id, asset_id, node_id, status,
		expires_at FROM download_authorizations WHERE id = ?`, id).
		Scan(&out.AuthorizationID, &out.AssetID, &out.NodeID, &out.State, &out.ExpiresAt)
	return out, err
}

func (s Store) routableAssetTx(ctx context.Context, tx *sql.Tx, assetID string) (string, string, int64, int64, error) {
	var nodeID, projectID string
	var size, multiplier int64
	err := tx.QueryRowContext(ctx, `SELECT n.id, r.project_id,
		COALESCE(NULLIF(p.download_multiplier, 0), 1), a.size_bytes FROM assets a
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		JOIN node_inventory ni ON ni.asset_id = a.id AND ni.state = 'verified'
		JOIN nodes n ON n.id = ni.node_id AND n.routing_ready = 1 AND n.state != 'disabled'
		WHERE a.id = ? AND a.service_state = 'candidate'
		ORDER BY COALESCE(n.last_heartbeat_at, '') DESC, n.id LIMIT 1`, assetID).
		Scan(&nodeID, &projectID, &multiplier, &size)
	return nodeID, projectID, multiplier, size, err
}

func insertAuthorization(ctx context.Context, tx *sql.Tx, id string, c Challenge, nodeID string, size int64, expires, reqID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?)`,
		id, c.AssetID, nodeID, c.ClientPrefixKey, nowText(), expires, size, 4, reqID)
	return err
}

func insertReservation(ctx context.Context, tx *sql.Tx, id, day string, size int64, now time.Time, scopes [2]quotaScope) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES (?, ?, ?, ?, 0, 'active', ?, ?, ?, ?, ?)`,
		id, day, size, size, now.Format(time.RFC3339Nano),
		scopes[0].Kind, scopes[0].Key, scopes[1].Kind, scopes[1].Key)
	return err
}

func consumeChallenge(ctx context.Context, tx *sql.Tx, id string) error {
	result, err := tx.ExecContext(ctx, `UPDATE challenges SET consumed_at = ?
		WHERE id = ? AND consumed_at IS NULL`, nowText(), id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
