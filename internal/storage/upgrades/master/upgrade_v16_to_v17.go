package master

import (
	"context"
	"database/sql"
	"strings"
)

func V16ToV17(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "client_blocks")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	for _, stmt := range []string{
		`ALTER TABLE client_blocks ADD COLUMN escalation_level INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE client_blocks ADD COLUMN punishment_active INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
