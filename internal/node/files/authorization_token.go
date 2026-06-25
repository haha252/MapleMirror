package files

import (
	"database/sql"
	"time"

	"mirror-server/internal/downloadtoken"
)

const opaqueTokenLength = 43
const opaqueAuthorizationWait = 2 * time.Second
const opaqueAuthorizationPoll = 50 * time.Millisecond

func (h *Handler) verifyDownloadToken(token string) (downloadtoken.Claims, error) {
	claims, err := h.Signer.Verify(token)
	if err == nil && claims.NodeID == h.NodeID {
		return claims, nil
	}
	if len(token) != opaqueTokenLength {
		return downloadtoken.Claims{}, err
	}
	deadline := time.Now().Add(opaqueAuthorizationWait)
	for {
		claims, lookupErr := h.loadOpaqueAuthorization(token)
		if lookupErr == nil {
			return claims, nil
		}
		if lookupErr != sql.ErrNoRows || time.Now().After(deadline) {
			return downloadtoken.Claims{}, lookupErr
		}
		time.Sleep(opaqueAuthorizationPoll)
	}
}

func (h *Handler) loadOpaqueAuthorization(token string) (downloadtoken.Claims, error) {
	var claims downloadtoken.Claims
	if h.DB == nil {
		return claims, sql.ErrNoRows
	}
	hash := downloadtoken.OpaqueHash(token)
	var status string
	err := h.DB.QueryRow(`SELECT authorization_id, asset_id, node_id,
		client_prefix_key, issued_at, expires_at, first_connection_timeout_seconds,
		idle_timeout_seconds, max_duration_seconds, max_bytes, traffic_limit_bytes,
		range_limit, request_id, status FROM local_authorizations
		WHERE token_hash = ?`, hash).
		Scan(&claims.AuthorizationID, &claims.AssetID, &claims.NodeID,
			&claims.ClientPrefix, &claims.IssuedAt, &claims.ExpiresAt,
			&claims.FirstConnectionSeconds, &claims.IdleTimeoutSeconds,
			&claims.MaxDurationSeconds, &claims.MaxBytes, &claims.TrafficLimitBytes,
			&claims.RangeConcurrencyLimit, &claims.RequestID, &status)
	if err != nil {
		return claims, err
	}
	claims.TokenVersion = downloadtoken.Version
	if claims.NodeID != h.NodeID {
		return claims, sql.ErrNoRows
	}
	if status == authorizationStatusExpiredFirstConnection ||
		status == authorizationStatusExpiredIdle ||
		status == authorizationStatusExpiredMaxDuration {
		return claims, errAuthorizationExpired
	}
	expires, err := time.Parse(time.RFC3339Nano, claims.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return claims, errAuthorizationExpired
	}
	return claims, nil
}
