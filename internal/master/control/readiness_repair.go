package control

import (
	"context"
	"time"

	"mirror-server/internal/master/assignment"
)

func (r Repository) RepairNodeReadiness(ctx context.Context) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := assignment.ReconcileReadiness(ctx, tx, "", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
