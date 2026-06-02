package public

import (
	"context"
	"database/sql"
	"time"
)

const (
	authorizationRouteWait = 3 * time.Second
	authorizationRoutePoll = 300 * time.Millisecond
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
