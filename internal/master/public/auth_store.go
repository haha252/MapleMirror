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
	asset, err := s.routableAssetTx(ctx, tx, c.AssetID, s.clientRegion(c.ClientPrefixKey))
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
