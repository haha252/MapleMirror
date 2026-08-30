package public

import (
	"context"
	"database/sql"
)

func insertDownloadHistory(ctx context.Context, tx *sql.Tx, authID string,
	c Challenge, asset routableAssetInfo, issued, expires, requestID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO download_history (
		authorization_id, client_prefix_key, source_kind, project_id, project_name,
		asset_id, file_name, version, system, architecture, node_id, node_name,
		issued_at, expires_at, status, request_id, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?, ?)`,
		authID, c.ClientPrefixKey, c.SourceKind, asset.ProjectID, asset.ProjectName,
		c.AssetID, asset.FileName, asset.Version, asset.System, asset.Architecture,
		asset.NodeID, asset.NodeName, issued, expires, requestID, issued)
	return err
}
