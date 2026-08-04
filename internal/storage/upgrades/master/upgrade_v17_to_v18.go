package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V17ToV18(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "nodes")
	if err != nil || !exists {
		return err
	}
	ok, err := upgrades.HasColumn(ctx, tx, "nodes", "region")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE nodes ADD COLUMN region TEXT NOT NULL DEFAULT 'unknown'`)
	return err
}
