package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V4ToV5(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "download_authorizations")
	if err != nil || !exists {
		return err
	}
	if err := addAuthorizationColumn(ctx, tx, "status_reason",
		"TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return addAuthorizationColumn(ctx, tx, "status_updated_at",
		"TEXT NOT NULL DEFAULT ''")
}

func addAuthorizationColumn(ctx context.Context, tx *sql.Tx, name, def string) error {
	ok, err := upgrades.HasColumn(ctx, tx, "download_authorizations", name)
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE download_authorizations ADD COLUMN `+name+` `+def)
	return err
}
