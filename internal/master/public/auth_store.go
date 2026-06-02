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
	NodeName                   string
	ProjectID                  string
	System                     string
	Architecture               string
	DownloadURL                string
	ExpiresAt                  string
	MaxBytes                   int64
	RangeLimit                 int
	RequestRemainingMicrounits map[string]int64
	TrafficRemainingBytes      map[string]int64
}

func (s *Store) IssueAuthorization(ctx context.Context, c Challenge, ttl time.Duration, reqID string) (IssuedAuthorization, AuthorizationDebug, error) {
	if !s.consumeChallenge(c.ID) {
		return IssuedAuthorization{}, AuthorizationDebug{}, sql.ErrNoRows
	}
	if err := s.waitForRoutableAsset(ctx, c.AssetID); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	defer tx.Rollback()
	asset, err := s.routableAssetTx(ctx, tx, c.AssetID)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	maxBytes := s.maxBytesPolicy().maxBytes(asset.SizeBytes)
	rangeLimit := s.rangeConcurrencyLimit()
	now := time.Now().UTC()
	scopes, err := quotaScopes(c.ClientPrefixKey)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	quota := s.Quota
	if quota.buckets == nil {
		quota = defaultQuota()
	}
	requestMultiplier := asset.Multiplier
	if requestMultiplier <= 0 {
		requestMultiplier = 1
	}
	exempt := quota.exempt(c.ClientPrefixKey)
	if exempt {
		requestMultiplier = 0
	}
	if err := quota.consume(ctx, tx, scopes, requestMultiplier, now); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	day := statDay(now, s.Location)
	reservationStatus := "active"
	if exempt {
		reservationStatus = "exempt"
	}
	if !exempt {
		if err := quota.reserve(ctx, tx, day, scopes, maxBytes); err != nil {
			return IssuedAuthorization{}, AuthorizationDebug{}, err
		}
	}
	authID, err := requestid.New()
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	expires := expiresAfter(ttl)
	if err := insertAuthorization(ctx, tx, authID, c, asset.NodeID, maxBytes, rangeLimit, expires, reqID); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	if err := insertReservation(ctx, tx, authID, day, maxBytes, reservationStatus, now, scopes); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	if err := upsertProjectStats(ctx, tx, day, asset.ProjectID, 1, 0, 0); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	if err := upsertAssetStats(ctx, tx, day, c.AssetID, 1, 0, 0, nowText()); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	requestRemaining, trafficRemaining, err := quota.snapshot(ctx, tx, day, scopes)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, err
	}
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version, AuthorizationID: authID,
		AssetID: c.AssetID, NodeID: asset.NodeID, ProjectID: asset.ProjectID,
		System: asset.System, Architecture: asset.Architecture, ClientPrefix: c.ClientPrefixKey,
		ExpiresAt: expires, MaxBytes: maxBytes, RangeConcurrencyLimit: rangeLimit, RequestID: reqID}
	debug := AuthorizationDebug{
		ClientPrefix:               c.ClientPrefixKey,
		NodeID:                     asset.NodeID,
		NodeName:                   asset.NodeName,
		ProjectID:                  asset.ProjectID,
		System:                     asset.System,
		Architecture:               asset.Architecture,
		DownloadURL:                asset.DownloadURL,
		ExpiresAt:                  expires,
		MaxBytes:                   maxBytes,
		RangeLimit:                 rangeLimit,
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

type routableAssetInfo struct {
	NodeID       string
	NodeName     string
	ProjectID    string
	DownloadURL  string
	System       string
	Architecture string
	Multiplier   int64
	SizeBytes    int64
}

func (s Store) routableAssetTx(ctx context.Context, tx *sql.Tx, assetID string) (routableAssetInfo, error) {
	var out routableAssetInfo
	var downloadBaseURL string
	err := tx.QueryRowContext(ctx, `SELECT n.id, n.public_name, r.project_id,
		n.public_download_base_url, COALESCE(NULLIF(p.download_multiplier, 0), 1),
		a.size_bytes, a.architecture, a.system FROM assets a
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id`+routableAssetReplicaSQL+`
		WHERE a.id = ? AND a.service_state = 'candidate'
		ORDER BY COALESCE(n.last_heartbeat_at, '') DESC, n.id LIMIT 1`, assetID).
		Scan(&out.NodeID, &out.NodeName, &out.ProjectID, &downloadBaseURL, &out.Multiplier,
			&out.SizeBytes, &out.Architecture, &out.System)
	if err != nil {
		return out, err
	}
	out.DownloadURL = joinDownloadURL(downloadBaseURL, assetID)
	return out, nil
}

func (s Store) rangeConcurrencyLimit() int {
	if s.RangeLimit <= 0 {
		return 32
	}
	return s.RangeLimit
}

func insertAuthorization(ctx context.Context, tx *sql.Tx, id string, c Challenge, nodeID string, size int64, rangeLimit int, expires, reqID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?)`,
		id, c.AssetID, nodeID, c.ClientPrefixKey, nowText(), expires, size, rangeLimit, reqID)
	return err
}

func insertReservation(ctx context.Context, tx *sql.Tx, id, day string, size int64, status string, now time.Time, scopes [2]quotaScope) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		id, day, size, size, status, now.Format(time.RFC3339Nano),
		scopes[0].Kind, scopes[0].Key, scopes[1].Kind, scopes[1].Key)
	return err
}
