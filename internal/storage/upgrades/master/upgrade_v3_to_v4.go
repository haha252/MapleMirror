package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V3ToV4(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "nodes")
	if err != nil || !exists {
		return err
	}
	ok, err := upgrades.HasColumn(ctx, tx, "nodes", "download_priority")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE nodes ADD COLUMN download_priority INTEGER NOT NULL DEFAULT 50`)
	return err
}
