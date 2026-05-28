package files

import (
	"time"

	"mirror-server/internal/downloadtoken"
)

func (h *Handler) recordTraffic(claims downloadtoken.Claims, assetID, nodeRequestID string, sent int64) error {
	next, err := h.nextEventSequence()
	if err != nil {
		return err
	}
	_, err = h.DB.Exec(`INSERT INTO pending_traffic_events
		(event_sequence, authorization_id, node_request_id, master_request_id,
		sent_bytes, created_at, asset_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'completed')`,
		next, claims.AuthorizationID, nodeRequestID, claims.RequestID,
		sent, time.Now().UTC().Format(time.RFC3339Nano), assetID)
	return err
}

func (h *Handler) nextEventSequence() (int64, error) {
	var next int64
	err := h.DB.QueryRow(`SELECT COALESCE(MAX(event_sequence), 0) + 1
		FROM pending_traffic_events`).Scan(&next)
	return next, err
}
