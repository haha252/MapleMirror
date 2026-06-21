package files

import (
	"database/sql"
	"errors"
	"time"

	"mirror-server/internal/downloadtoken"
)

const (
	authorizationStatusActive                 = "active"
	authorizationStatusExpiredFirstConnection = "expired_first_connection"
	authorizationStatusExpiredIdle            = "expired_idle"
	authorizationStatusExpiredMaxDuration     = "expired_max_duration"
)

var errAuthorizationExpired = errors.New("download authorization expired")

type authorizationTiming struct {
	StartedAt   time.Time
	LastWriteAt time.Time
	MaxDeadline time.Time
	IdleTimeout time.Duration
}

func (h *Handler) beginAuthorization(claims downloadtoken.Claims) (authorizationTiming, error) {
	issued, expires, ok := claims.Timing()
	if !ok {
		return authorizationTiming{StartedAt: time.Now().UTC(),
			LastWriteAt: time.Now().UTC()}, nil
	}
	now := time.Now().UTC()
	firstTimeout := time.Duration(claims.FirstConnectionSeconds) * time.Second
	idleTimeout := time.Duration(claims.IdleTimeoutSeconds) * time.Second
	maxDuration := time.Duration(claims.MaxDurationSeconds) * time.Second
	maxDeadline := issued.Add(maxDuration)
	if maxDuration <= 0 {
		maxDeadline = expires
	}
	state, exists, err := h.loadAuthorizationState(claims.AuthorizationID)
	if err != nil {
		return authorizationTiming{}, err
	}
	if exists && state.expired() {
		return authorizationTiming{}, errAuthorizationExpired
	}
	if now.After(maxDeadline) {
		return authorizationTiming{}, h.expireAuthorization(claims, authorizationStatusExpiredMaxDuration, now)
	}
	if !exists || state.firstSeenAt == "" {
		if firstTimeout > 0 && now.After(issued.Add(firstTimeout)) {
			return authorizationTiming{}, h.expireAuthorization(claims, authorizationStatusExpiredFirstConnection, now)
		}
		if err := h.upsertAuthorizationActive(claims, issued, expires, now, now); err != nil {
			return authorizationTiming{}, err
		}
		return authorizationTiming{StartedAt: issued, LastWriteAt: now,
			MaxDeadline: maxDeadline, IdleTimeout: idleTimeout}, nil
	}
	lastActivity, _ := time.Parse(time.RFC3339Nano, state.lastActivityAt)
	if idleTimeout > 0 && !lastActivity.IsZero() && now.Sub(lastActivity) > idleTimeout {
		return authorizationTiming{}, h.expireAuthorization(claims, authorizationStatusExpiredIdle, now)
	}
	if err := h.touchAuthorization(claims.AuthorizationID, now); err != nil {
		return authorizationTiming{}, err
	}
	return authorizationTiming{StartedAt: issued, LastWriteAt: now,
		MaxDeadline: maxDeadline, IdleTimeout: idleTimeout}, nil
}

func (h *Handler) touchAuthorization(id string, at time.Time) error {
	if h.DB == nil {
		return nil
	}
	_, err := h.DB.Exec(`UPDATE local_authorizations
		SET last_activity_at = ?, updated_at = ? WHERE authorization_id = ?`,
		at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano), id)
	return err
}

func (h *Handler) expireAuthorization(claims downloadtoken.Claims, status string, at time.Time) error {
	issued, expires, _ := claims.Timing()
	if issued.IsZero() {
		issued = at
	}
	if expires.IsZero() {
		expires = at
	}
	if h.DB == nil {
		return errAuthorizationExpired
	}
	_, err := h.DB.Exec(`INSERT INTO local_authorizations
		(authorization_id, asset_id, node_id, issued_at, expires_at, first_seen_at,
		 last_activity_at, status, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)
		ON CONFLICT(authorization_id) DO UPDATE SET
			status = excluded.status,
			reason = excluded.reason,
			last_activity_at = excluded.last_activity_at,
			updated_at = excluded.updated_at,
			reported_at = NULL`,
		claims.AuthorizationID, claims.AssetID, claims.NodeID,
		issued.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano),
		at.Format(time.RFC3339Nano), status, status,
		at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return errAuthorizationExpired
}

func (h *Handler) upsertAuthorizationActive(claims downloadtoken.Claims,
	issued, expires, first, last time.Time) error {
	if h.DB == nil {
		return nil
	}
	nowText := last.Format(time.RFC3339Nano)
	_, err := h.DB.Exec(`INSERT INTO local_authorizations
		(authorization_id, asset_id, node_id, issued_at, expires_at, first_seen_at,
		 last_activity_at, status, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
		ON CONFLICT(authorization_id) DO UPDATE SET
			first_seen_at = COALESCE(NULLIF(local_authorizations.first_seen_at, ''), excluded.first_seen_at),
			last_activity_at = excluded.last_activity_at,
			status = excluded.status,
			updated_at = excluded.updated_at`,
		claims.AuthorizationID, claims.AssetID, claims.NodeID,
		issued.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano),
		first.Format(time.RFC3339Nano), last.Format(time.RFC3339Nano),
		authorizationStatusActive, nowText, nowText)
	return err
}

type localAuthorizationState struct {
	status         string
	firstSeenAt    string
	lastActivityAt string
}

func (s localAuthorizationState) expired() bool {
	return s.status == authorizationStatusExpiredFirstConnection ||
		s.status == authorizationStatusExpiredIdle ||
		s.status == authorizationStatusExpiredMaxDuration
}

func (h *Handler) loadAuthorizationState(id string) (localAuthorizationState, bool, error) {
	if h.DB == nil {
		return localAuthorizationState{}, false, nil
	}
	var state localAuthorizationState
	err := h.DB.QueryRow(`SELECT status, COALESCE(first_seen_at, ''),
		COALESCE(last_activity_at, '') FROM local_authorizations
		WHERE authorization_id = ?`, id).Scan(&state.status, &state.firstSeenAt,
		&state.lastActivityAt)
	if err == sql.ErrNoRows {
		return state, false, nil
	}
	return state, err == nil, err
}
