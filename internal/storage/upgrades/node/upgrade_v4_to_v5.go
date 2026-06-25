package node

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V4ToV5(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct {
		name string
		def  string
	}{
		{"token_hash", "TEXT NOT NULL DEFAULT ''"},
		{"client_prefix_key", "TEXT NOT NULL DEFAULT ''"},
		{"max_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"traffic_limit_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"range_limit", "INTEGER NOT NULL DEFAULT 0"},
		{"request_id", "TEXT NOT NULL DEFAULT ''"},
		{"first_connection_timeout_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"idle_timeout_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"max_duration_seconds", "INTEGER NOT NULL DEFAULT 0"},
	} {
		ok, err := upgrades.HasColumn(ctx, tx, "local_authorizations", column.name)
		if err != nil || ok {
			if err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`ALTER TABLE local_authorizations ADD COLUMN `+column.name+` `+column.def); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_local_authorizations_token_hash
		ON local_authorizations(token_hash)`)
	return err
}
