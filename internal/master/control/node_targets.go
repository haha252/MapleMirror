package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/assignment"
)

func seedNodeTargets(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	if err := assignment.ReconcileNode(ctx, tx, nodeID, now); err != nil {
		return err
	}
	_, err := assignment.GenerateNodeTasks(ctx, tx, nodeID, now)
	return err
}
