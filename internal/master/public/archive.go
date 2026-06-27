package public

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/master/accountingarchive"
)

func (s Store) archiveAuthorizationIssued(ctx context.Context,
	record accountingarchive.AuthorizationRecord, committed time.Time) {
	if s.Archive == nil {
		return
	}
	if err := s.Archive.WriteAuthorization(ctx, record, committed); err != nil && s.Logger != nil {
		s.Logger.Warn(ctx, "授权签发归档写入失败",
			slog.String("authorization_id", record.AuthorizationID),
			slog.String("node_id", record.NodeID),
			slog.String("error", err.Error()))
	}
}

func authorizationArchiveRecord(authID string, challenge Challenge, asset routableAssetInfo,
	issued, expires string, maxBytes, trafficLimit int64, rangeLimit int, requestID, tokenHash string,
	firstConnectionSeconds, idleTimeoutSeconds, maxDurationSeconds int) accountingarchive.AuthorizationRecord {
	return accountingarchive.AuthorizationRecord{
		AuthorizationID:               authID,
		AssetID:                       challenge.AssetID,
		NodeID:                        asset.NodeID,
		ClientPrefixKey:               challenge.ClientPrefixKey,
		SourceKind:                    authorizationSourceKind(challenge.Kind),
		ProjectID:                     asset.ProjectID,
		System:                        asset.System,
		Architecture:                  asset.Architecture,
		IssuedAt:                      issued,
		ExpiresAt:                     expires,
		MaxBytes:                      maxBytes,
		TrafficLimitBytes:             trafficLimit,
		RangeLimit:                    rangeLimit,
		RequestID:                     requestID,
		TokenHash:                     tokenHash,
		FirstConnectionTimeoutSeconds: firstConnectionSeconds,
		IdleTimeoutSeconds:            idleTimeoutSeconds,
		MaxDurationSeconds:            maxDurationSeconds,
	}
}
