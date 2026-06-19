package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V2ToV3(ctx context.Context, tx *sql.Tx) error {
	columns := []struct {
		table string
		name  string
		sql   string
	}{
		{"releases", "source_type", `ALTER TABLE releases ADD COLUMN source_type TEXT NOT NULL DEFAULT 'github_releases'`},
		{"releases", "source_release_key", `ALTER TABLE releases ADD COLUMN source_release_key TEXT NOT NULL DEFAULT ''`},
		{"assets", "source_type", `ALTER TABLE assets ADD COLUMN source_type TEXT NOT NULL DEFAULT 'github_releases'`},
		{"assets", "source_asset_key", `ALTER TABLE assets ADD COLUMN source_asset_key TEXT NOT NULL DEFAULT ''`},
		{"assets", "variant", `ALTER TABLE assets ADD COLUMN variant TEXT NOT NULL DEFAULT ''`},
		{"assets", "display_label", `ALTER TABLE assets ADD COLUMN display_label TEXT NOT NULL DEFAULT ''`},
		{"assets", "priority", `ALTER TABLE assets ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`},
		{"assets", "labels_json", `ALTER TABLE assets ADD COLUMN labels_json TEXT NOT NULL DEFAULT ''`},
		{"assets", "classification_reason", `ALTER TABLE assets ADD COLUMN classification_reason TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		exists, err := tableExists(ctx, tx, column.table)
		if err != nil || !exists {
			if err != nil {
				return err
			}
			continue
		}
		ok, err := upgrades.HasColumn(ctx, tx, column.table, column.name)
		if err != nil || ok {
			if err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, column.sql); err != nil {
			return err
		}
	}
	return nil
}

func tableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = ?`, table).Scan(&count)
	return count > 0, err
}
