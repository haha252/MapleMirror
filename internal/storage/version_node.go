package storage

import (
	"context"
	"database/sql"
)

func ensureNodeV1IdentityMaterials(ctx context.Context, tx *sql.Tx) error {
	columns := []struct {
		name string
		def  string
	}{
		{"certificate_pem", "TEXT"},
		{"ca_pem", "TEXT"},
		{"private_key_pem", "TEXT"},
		{"download_token_public_key_pem", "TEXT"},
	}
	for _, column := range columns {
		ok, err := hasColumn(ctx, tx, "control_identity", column.name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`ALTER TABLE control_identity ADD COLUMN `+column.name+` `+column.def); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS node_enrollment_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enrollment_id TEXT,
		pairing_code TEXT,
		private_key_pem TEXT,
		ca_pem TEXT,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	ok, err := hasColumn(ctx, tx, "inventory_report_cursor", "force_report_requested_at")
	if err != nil {
		return err
	}
	if !ok {
		if _, err = tx.ExecContext(ctx,
			`ALTER TABLE inventory_report_cursor ADD COLUMN force_report_requested_at TEXT`); err != nil {
			return err
		}
	}
	return ensureNodeV3PeerFallbackResults(ctx, tx)
}

func upgradeNode1To2(ctx context.Context, tx *sql.Tx) error {
	ok, err := hasColumn(ctx, tx, "inventory_report_cursor", "force_report_requested_at")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE inventory_report_cursor ADD COLUMN force_report_requested_at TEXT`)
	return err
}

func upgradeNode2To3(ctx context.Context, tx *sql.Tx) error {
	return ensureNodeV3PeerFallbackResults(ctx, tx)
}

func ensureNodeV3PeerFallbackResults(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS pending_sync_task_results (
		task_id TEXT PRIMARY KEY,
		asset_id TEXT,
		result TEXT NOT NULL,
		local_digest_sha256 TEXT,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		message TEXT,
		peer_fallback_attempted INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		reported_at TEXT
	)`); err != nil {
		return err
	}
	ok, err := hasColumn(ctx, tx, "pending_sync_task_results", "peer_fallback_attempted")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE pending_sync_task_results ADD COLUMN peer_fallback_attempted INTEGER NOT NULL DEFAULT 0`)
	return err
}

func hasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
