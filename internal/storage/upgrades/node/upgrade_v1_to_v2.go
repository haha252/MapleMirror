package node

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V1ToV2(ctx context.Context, tx *sql.Tx) error {
	ok, err := upgrades.HasColumn(ctx, tx, "inventory_report_cursor", "force_report_requested_at")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE inventory_report_cursor ADD COLUMN force_report_requested_at TEXT`)
	return err
}
