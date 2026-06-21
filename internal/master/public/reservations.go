package public

import (
	"context"
	"database/sql"
	"time"
)

func insertReservation(ctx context.Context, tx *sql.Tx, id, day string, size int64,
	status string, now time.Time, scopes [2]quotaScope) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		id, day, size, size, status, now.Format(time.RFC3339Nano),
		scopes[0].Kind, scopes[0].Key, scopes[1].Kind, scopes[1].Key)
	return err
}
