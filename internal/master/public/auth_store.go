package public

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
)

var errChallengeBusy = errors.New("challenge authorization in progress")

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

func (s *Store) IssueAuthorization(ctx context.Context, c Challenge, lifetime TokenLifetime, reqID string) (IssuedAuthorization, AuthorizationDebug, error) {
	auth, debug, _, err := s.issueAuthorization(ctx, c, lifetime.normalized(), reqID, nil)
	return auth, debug, err
}

func (s *Store) IssueSignedAuthorization(ctx context.Context, c Challenge, lifetime TokenLifetime, reqID string,
	sign func(downloadtoken.Claims) (string, error)) (IssuedAuthorization, AuthorizationDebug, string, error) {
	return s.issueAuthorization(ctx, c, lifetime.normalized(), reqID, sign)
}

func (s *Store) issueAuthorization(ctx context.Context, c Challenge, lifetime TokenLifetime, reqID string,
	sign func(downloadtoken.Claims) (string, error)) (IssuedAuthorization, AuthorizationDebug, string, error) {
	locked, ok, busy := s.beginChallenge(c.ID)
	if busy {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", errChallengeBusy
	}
	if !ok || locked.SourceKind != c.SourceKind || locked.ProtocolVersion != c.ProtocolVersion ||
		locked.Algorithm != c.Algorithm || locked.AssetID != c.AssetID ||
		locked.ClientPrefixKey != c.ClientPrefixKey {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", sql.ErrNoRows
	}
	defer s.releaseChallenge(c.ID)
	if err := s.waitForRoutableAsset(ctx, c.AssetID); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	defer tx.Rollback()
	asset, err := s.routableAssetTx(ctx, tx, c.AssetID)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	maxBytes := s.maxBytesPolicy().maxBytes(asset.SizeBytes)
	trafficLimit := maxBytes
	rangeLimit := s.rangeConcurrencyLimit()
	now := time.Now().UTC()
	scopes, err := quotaScopes(c.ClientPrefixKey)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
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
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	day := statDay(now, s.Location)
	reservationStatus := "active"
	if exempt {
		reservationStatus = "exempt"
	}
	if !exempt {
		reservedBytes, err := quota.reserve(ctx, tx, day, scopes, maxBytes)
		if err != nil {
			return IssuedAuthorization{}, AuthorizationDebug{}, "", err
		}
		trafficLimit = reservedBytes
	}
	authID, err := requestid.New()
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	issued := now.Format(time.RFC3339Nano)
	expires := expiresAfter(now, lifetime.MaxDuration)
	firstConnectionSeconds := durationSeconds(lifetime.FirstConnectionTimeout)
	idleTimeoutSeconds := durationSeconds(lifetime.IdleTimeout)
	maxDurationSeconds := durationSeconds(lifetime.MaxDuration)
	var token string
	if sign != nil {
		token, err = sign(downloadtoken.Claims{AuthorizationID: authID})
		if err != nil {
			return IssuedAuthorization{}, AuthorizationDebug{}, "", err
		}
	}
	tokenHash := ""
	if token != "" {
		tokenHash = downloadtoken.OpaqueHash(token)
	}
	if err := insertAuthorization(ctx, tx, authID, c, asset.NodeID, maxBytes,
		trafficLimitBytes(trafficLimit, exempt), rangeLimit, issued, expires,
		firstConnectionSeconds, idleTimeoutSeconds, maxDurationSeconds,
		reqID, tokenHash); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	if err := insertReservation(ctx, tx, authID, day, trafficLimit, reservationStatus, now, scopes); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	webAuth, apiAuth := authorizationSourceIncrements(c.SourceKind)
	if err := upsertProjectStats(ctx, tx, day, asset.ProjectID, 1, webAuth, apiAuth, 0, 0); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	if err := upsertAssetStats(ctx, tx, day, c.AssetID, 1, webAuth, apiAuth, 0, 0, issued); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	if s.StatsBuffer == nil {
		if err := addPublicStatCounters(ctx, tx, day, 0, 1, webAuth, apiAuth, 0, 0, issued); err != nil {
			return IssuedAuthorization{}, AuthorizationDebug{}, "", err
		}
		if err := addAssetStatCounters(ctx, tx, c.AssetID, 1, webAuth, apiAuth, 0, 0, issued); err != nil {
			return IssuedAuthorization{}, AuthorizationDebug{}, "", err
		}
		if err := addProjectStatCounters(ctx, tx, asset.ProjectID, 1, webAuth, apiAuth, 0, 0, issued); err != nil {
			return IssuedAuthorization{}, AuthorizationDebug{}, "", err
		}
	}
	requestRemaining, trafficRemaining, err := quota.snapshot(ctx, tx, day, scopes)
	if err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version, AuthorizationID: authID,
		AssetID: c.AssetID, NodeID: asset.NodeID, ProjectID: asset.ProjectID,
		System: asset.System, Architecture: asset.Architecture, ClientPrefix: c.ClientPrefixKey,
		IssuedAt: issued, ExpiresAt: expires,
		FirstConnectionSeconds: firstConnectionSeconds,
		IdleTimeoutSeconds:     idleTimeoutSeconds,
		MaxDurationSeconds:     maxDurationSeconds,
		MaxBytes:               maxBytes, TrafficLimitBytes: trafficLimitBytes(trafficLimit, exempt),
		RangeConcurrencyLimit: rangeLimit, RequestID: reqID}
	archiveRecord := authorizationArchiveRecord(authID, c, asset, issued, expires, maxBytes,
		trafficLimitBytes(trafficLimit, exempt), rangeLimit, reqID, tokenHash,
		firstConnectionSeconds, idleTimeoutSeconds, maxDurationSeconds)
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
	if err := tx.Commit(); err != nil {
		return IssuedAuthorization{}, AuthorizationDebug{}, "", err
	}
	s.archiveAuthorizationIssued(ctx, archiveRecord, time.Now().UTC())
	if tokenHash != "" {
		s.notifyAuthorizationDelivery(asset.NodeID)
	}
	s.bufferAuthorizationStats(day, c.AssetID, asset.ProjectID, webAuth, apiAuth)
	s.finishChallenge(c.ID)
	return IssuedAuthorization{Claims: claims}, debug, token, nil
}

type routableAssetInfo struct {
	NodeID        string
	NodeName      string
	Priority      int
	LastHeartbeat string
	ProjectID     string
	Version       string
	FileName      string
	DownloadURL   string
	System        string
	Architecture  string
	Multiplier    int64
	SizeBytes     int64
}

func (s Store) routableAssetTx(ctx context.Context, tx *sql.Tx, assetID string) (routableAssetInfo, error) {
	var out routableAssetInfo
	args := append(s.routableAssetReplicaArgs(), assetID)
	rows, err := tx.QueryContext(ctx, `SELECT n.id, n.public_name, n.download_priority,
		COALESCE(n.last_heartbeat_at, ''), r.project_id,
		r.tag_name, a.file_name, n.public_download_base_url, COALESCE(NULLIF(p.download_multiplier, 0), 1),
		a.size_bytes, a.architecture, a.system FROM assets a
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id`+routableAssetReplicaSQL+`
		WHERE a.id = ? AND a.service_state = 'candidate'
		AND r.selected = 1 AND p.enabled = 1
		ORDER BY n.download_priority DESC, COALESCE(n.last_heartbeat_at, '') DESC, n.id`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	candidates := make([]routableAssetInfo, 0, 4)
	for rows.Next() {
		var item routableAssetInfo
		var downloadBaseURL string
		if err := rows.Scan(&item.NodeID, &item.NodeName, &item.Priority,
			&item.LastHeartbeat, &item.ProjectID,
			&item.Version, &item.FileName, &downloadBaseURL, &item.Multiplier,
			&item.SizeBytes, &item.Architecture, &item.System); err != nil {
			return out, err
		}
		item.DownloadURL, err = joinDownloadURL(downloadBaseURL, item.ProjectID, item.Version, item.FileName)
		if err == nil {
			candidates = append(candidates, item)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return s.selectRoutableAsset(candidates)
}

func (s Store) rangeConcurrencyLimit() int {
	if s.RangeLimit <= 0 {
		return 32
	}
	return s.RangeLimit
}
