package master

import (
	"context"
	"database/sql"
)

func V6ToV7(ctx context.Context, tx *sql.Tx) error {
	for _, item := range []struct {
		table     string
		statement string
	}{
		{
			table: "node_availability_samples",
			statement: `CREATE INDEX IF NOT EXISTS idx_node_availability_samples_window
				ON node_availability_samples(sample_start, node_id)`,
		},
		{
			table: "daily_node_traffic_stats",
			statement: `CREATE INDEX IF NOT EXISTS idx_daily_node_traffic_stats_node
				ON daily_node_traffic_stats(node_id, stat_day)`,
		},
	} {
		exists, err := tableExists(ctx, tx, item.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, item.statement); err != nil {
			return err
		}
	}
	return nil
}
