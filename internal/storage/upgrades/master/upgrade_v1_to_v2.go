package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V1ToV2(ctx context.Context, tx *sql.Tx) error {
	ok, err := upgrades.HasColumn(ctx, tx, "admin_ip_blocks", "display_ip")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE admin_ip_blocks ADD COLUMN display_ip TEXT NOT NULL DEFAULT ''`)
	return err
}
