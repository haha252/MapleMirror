package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V9ToV10(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfTableExists(ctx, tx, "download_authorizations", "source_kind",
		"TEXT NOT NULL DEFAULT 'web' CHECK (source_kind IN ('web', 'api'))"); err != nil {
		return err
	}
	if err := addColumnIfTableExists(ctx, tx, "daily_project_stats", "web_authorization_count",
		"INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfTableExists(ctx, tx, "daily_project_stats", "api_authorization_count",
		"INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfTableExists(ctx, tx, "daily_asset_stats", "web_authorization_count",
		"INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfTableExists(ctx, tx, "daily_asset_stats", "api_authorization_count",
		"INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	exists, err := tableExists(ctx, tx, "daily_project_stats")
	if err != nil {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE daily_project_stats
			SET web_authorization_count = authorization_count
			WHERE web_authorization_count = 0 AND api_authorization_count = 0
			AND authorization_count != 0`); err != nil {
			return err
		}
	}
	exists, err = tableExists(ctx, tx, "daily_asset_stats")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE daily_asset_stats
		SET web_authorization_count = authorization_count
		WHERE web_authorization_count = 0 AND api_authorization_count = 0
		AND authorization_count != 0`)
	return err
}

func addColumnIfTableExists(ctx context.Context, tx *sql.Tx, table, name, def string) error {
	exists, err := tableExists(ctx, tx, table)
	if err != nil || !exists {
		return err
	}
	ok, err := upgrades.HasColumn(ctx, tx, table, name)
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+def)
	return err
}
