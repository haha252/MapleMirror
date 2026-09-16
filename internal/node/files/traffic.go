package files

import (
	"net/http"
	"time"

	"mirror-server/internal/downloadtoken"
)

type pendingTrafficEvent struct {
	Claims        downloadtoken.Claims
	AssetID       string
	NodeRequestID string
	Sent          int64
}

func (h *Handler) recordTraffic(claims downloadtoken.Claims, assetID, nodeRequestID string, sent int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	event := pendingTrafficEvent{Claims: claims, AssetID: assetID,
		NodeRequestID: nodeRequestID, Sent: sent}
	if err := h.insertTrafficEventLocked(event); err != nil {
		h.pendingTrafficLocked(claims.AuthorizationID, event)
		return err
	}
	return nil
}

func (h *Handler) retryPendingTraffic(authorizationID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	pending := h.pendingTraffic[authorizationID]
	for len(pending) > 0 {
		if err := h.insertTrafficEventLocked(pending[0]); err != nil {
			h.pendingTraffic[authorizationID] = pending
			return err
		}
		pending = pending[1:]
	}
	delete(h.pendingTraffic, authorizationID)
	return nil
}

func (h *Handler) rejectPendingTraffic(w http.ResponseWriter, r *http.Request, authorizationID string) bool {
	if err := h.retryPendingTraffic(authorizationID); err != nil {
		httpError(w, r, http.StatusForbidden, "授权流量事件等待补写")
		return true
	}
	return false
}

func (h *Handler) hasPendingTrafficLocked(id string) bool {
	return len(h.pendingTraffic[id]) > 0
}

func (h *Handler) pendingTrafficLocked(id string, event pendingTrafficEvent) {
	if h.pendingTraffic == nil {
		h.pendingTraffic = make(map[string][]pendingTrafficEvent)
	}
	h.pendingTraffic[id] = append(h.pendingTraffic[id], event)
}

func (h *Handler) insertTrafficEventLocked(event pendingTrafficEvent) error {
	next, err := h.nextEventSequence()
	if err != nil {
		return err
	}
	_, err = h.DB.Exec(`INSERT INTO pending_traffic_events
		(event_sequence, authorization_id, node_request_id, master_request_id,
		sent_bytes, created_at, asset_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'completed')`,
		next, event.Claims.AuthorizationID, event.NodeRequestID, event.Claims.RequestID,
		event.Sent, time.Now().UTC().Format(time.RFC3339Nano), event.AssetID)
	if err == nil && h.EventWake != nil {
		h.EventWake.Wake()
	}
	return err
}

func (h *Handler) nextEventSequence() (int64, error) {
	var next int64
	err := h.DB.QueryRow(`SELECT COALESCE(MAX(event_sequence), 0) + 1
		FROM pending_traffic_events`).Scan(&next)
	return next, err
}
