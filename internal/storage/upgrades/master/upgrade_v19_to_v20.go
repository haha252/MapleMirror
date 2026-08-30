package master

import (
	"context"
	"database/sql"
)

func V19ToV20(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS download_history (
		authorization_id TEXT PRIMARY KEY,
		client_prefix_key TEXT NOT NULL,
		source_kind TEXT NOT NULL DEFAULT 'web',
		project_id TEXT NOT NULL,
		project_name TEXT NOT NULL,
		asset_id TEXT NOT NULL,
		file_name TEXT NOT NULL,
		version TEXT NOT NULL,
		system TEXT NOT NULL,
		architecture TEXT NOT NULL,
		node_id TEXT NOT NULL,
		node_name TEXT NOT NULL,
		issued_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		first_transfer_at TEXT NOT NULL DEFAULT '',
		last_transfer_at TEXT NOT NULL DEFAULT '',
		sent_bytes INTEGER NOT NULL DEFAULT 0 CHECK(sent_bytes >= 0),
		status TEXT NOT NULL DEFAULT 'issued',
		status_reason TEXT NOT NULL DEFAULT '',
		request_id TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_download_history_client_issued
		ON download_history(client_prefix_key, issued_at DESC);
	CREATE INDEX IF NOT EXISTS idx_download_history_issued
		ON download_history(issued_at);
	CREATE INDEX IF NOT EXISTS idx_download_history_project_issued
		ON download_history(project_id, issued_at DESC);`); err != nil {
		return err
	}
	ok, err := allTablesExist(ctx, tx, []string{
		"download_authorizations", "assets", "releases", "projects", "nodes", "traffic_reservations",
	})
	if err != nil || !ok {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO download_history (
		authorization_id, client_prefix_key, source_kind, project_id, project_name,
		asset_id, file_name, version, system, architecture, node_id, node_name,
		issued_at, expires_at, first_transfer_at, sent_bytes, status, status_reason,
		request_id, updated_at
	)
	SELECT da.id, da.client_prefix_key, COALESCE(NULLIF(da.source_kind, ''), 'web'),
		r.project_id, p.name, a.id, a.file_name, r.tag_name, a.system, a.architecture,
		da.node_id, COALESCE(NULLIF(n.public_name, ''), da.node_id),
		da.issued_at, da.expires_at, COALESCE(da.first_transfer_at, ''),
		COALESCE(tr.settled_bytes, 0), da.status, COALESCE(da.status_reason, ''),
		da.request_id, COALESCE(NULLIF(da.status_updated_at, ''), da.issued_at)
	FROM download_authorizations da
	JOIN assets a ON a.id = da.asset_id
	JOIN releases r ON r.id = a.release_id
	JOIN projects p ON p.id = r.project_id
	JOIN nodes n ON n.id = da.node_id
	LEFT JOIN traffic_reservations tr ON tr.authorization_id = da.id
	WHERE da.issued_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-7 days')`)
	return err
}
