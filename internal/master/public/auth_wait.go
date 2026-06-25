package public

import (
	"context"
	"database/sql"
	"time"
)

const (
	authorizationRouteWait    = 3 * time.Second
	authorizationRoutePoll    = 300 * time.Millisecond
	authorizationDeliveryPoll = 200 * time.Millisecond
)

func (s Store) waitForRoutableAsset(ctx context.Context, assetID string) error {
	if _, err := s.routableAsset(ctx, assetID); err != sql.ErrNoRows {
		return err
	}
	deadline := time.Now().Add(authorizationRouteWait)
	for {
		sleep := authorizationRoutePoll
		if remaining := time.Until(deadline); remaining <= 0 {
			return sql.ErrNoRows
		} else if remaining < sleep {
			sleep = remaining
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if _, err := s.routableAsset(ctx, assetID); err != sql.ErrNoRows {
			return err
		}
	}
}

func (s Store) waitForAuthorizationDelivered(ctx context.Context,
	authorizationID, nodeID string) error {
	for {
		delivered, err := s.authorizationDelivered(ctx, authorizationID, nodeID)
		if err != nil || delivered {
			return err
		}
		s.notifyAuthorizationDelivery(nodeID)
		timer := time.NewTimer(authorizationDeliveryPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s Store) authorizationExpiresAt(ctx context.Context,
	authorizationID, nodeID string) (string, error) {
	var expiresAt string
	err := s.DB.QueryRowContext(ctx, `SELECT expires_at
		FROM download_authorizations WHERE id = ? AND node_id = ?`,
		authorizationID, nodeID).Scan(&expiresAt)
	return expiresAt, err
}

func (s Store) authorizationDelivered(ctx context.Context,
	authorizationID, nodeID string) (bool, error) {
	var deliveredAt string
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(delivered_at, '')
		FROM download_authorizations WHERE id = ? AND node_id = ?`,
		authorizationID, nodeID).Scan(&deliveredAt)
	if err != nil {
		return false, err
	}
	return deliveredAt != "", nil
}
