package public

import (
	"context"
	"database/sql"
)

func insertAuthorization(ctx context.Context, tx *sql.Tx, id string, c Challenge,
	nodeID string, size, trafficLimit int64, rangeLimit int, issued, expires string,
	firstConnectionSeconds, idleTimeoutSeconds, maxDurationSeconds int,
	reqID, tokenHash string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, traffic_limit_bytes, range_limit, status, request_id, token_hash,
		first_connection_timeout_seconds, idle_timeout_seconds, max_duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?, ?, ?, ?, ?)`,
		id, c.AssetID, nodeID, c.ClientPrefixKey, issued, expires, size,
		trafficLimit, rangeLimit, reqID, tokenHash,
		firstConnectionSeconds, idleTimeoutSeconds, maxDurationSeconds)
	return err
}
