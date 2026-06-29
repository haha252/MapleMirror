package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V12ToV13(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "projects")
	if err != nil || !exists {
		return err
	}
	columns := []struct {
		name string
		sql  string
	}{
		{"description", `ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT ''`},
		{"homepage_url", `ALTER TABLE projects ADD COLUMN homepage_url TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		ok, err := upgrades.HasColumn(ctx, tx, "projects", column.name)
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
