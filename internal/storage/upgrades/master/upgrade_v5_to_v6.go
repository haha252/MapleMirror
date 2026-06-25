package master

import (
	"context"
	"database/sql"
)

func V5ToV6(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "download_authorizations")
	if err != nil || !exists {
		return err
	}
	for _, column := range []struct {
		name string
		def  string
	}{
		{"token_hash", "TEXT NOT NULL DEFAULT ''"},
		{"traffic_limit_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"first_connection_timeout_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"idle_timeout_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"max_duration_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"delivered_at", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addAuthorizationColumn(ctx, tx, column.name, column.def); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_download_authorizations_delivery
		ON download_authorizations(node_id, delivered_at, issued_at)`)
	return err
}
