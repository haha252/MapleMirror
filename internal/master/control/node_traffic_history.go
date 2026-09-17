package control

import (
	"context"
	"database/sql"
	"time"
)

func archiveNodeTraffic(ctx context.Context, tx *sql.Tx, nodeID string) error {
	deletedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO historical_node_traffic_totals
		(node_id, public_name, sent_bytes, deleted_at)
		SELECT n.id, n.public_name,
			MAX(COALESCE(t.sent_bytes, 0), COALESCE((
				SELECT SUM(d.sent_bytes) FROM daily_node_traffic_stats d WHERE d.node_id = n.id
			), 0)), ?
		FROM nodes n
		LEFT JOIN node_traffic_totals t ON t.node_id = n.id
		WHERE n.id = ?
		ON CONFLICT(node_id) DO UPDATE SET
			public_name = excluded.public_name,
			sent_bytes = MAX(historical_node_traffic_totals.sent_bytes, excluded.sent_bytes),
			deleted_at = excluded.deleted_at`, deletedAt, nodeID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO historical_daily_node_traffic_stats
		(stat_day, node_id, sent_bytes, deleted_at)
		SELECT stat_day, node_id, sent_bytes, ?
		FROM daily_node_traffic_stats
		WHERE node_id = ?
		ON CONFLICT(stat_day, node_id) DO UPDATE SET
			sent_bytes = MAX(historical_daily_node_traffic_stats.sent_bytes, excluded.sent_bytes),
			deleted_at = excluded.deleted_at`, deletedAt, nodeID)
	return err
}
