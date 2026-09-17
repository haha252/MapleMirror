package master

import (
	"context"
	"database/sql"
)

func V21ToV22(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS historical_node_traffic_totals (
		node_id TEXT PRIMARY KEY,
		public_name TEXT NOT NULL,
		sent_bytes INTEGER NOT NULL DEFAULT 0 CHECK(sent_bytes >= 0),
		deleted_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS historical_daily_node_traffic_stats (
		stat_day TEXT NOT NULL,
		node_id TEXT NOT NULL,
		sent_bytes INTEGER NOT NULL DEFAULT 0 CHECK(sent_bytes >= 0),
		deleted_at TEXT NOT NULL,
		PRIMARY KEY (stat_day, node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_historical_daily_node_traffic_day
		ON historical_daily_node_traffic_stats(stat_day);`)
	return err
}
