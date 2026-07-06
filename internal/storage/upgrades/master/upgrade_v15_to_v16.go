package master

import (
	"context"
	"database/sql"
)

// V15ToV16 将高频运行历史收缩为每节点最新状态。完整历史改由 JSONL 归档承担。
func V15ToV16(ctx context.Context, tx *sql.Tx) error {
	statements := []struct{ table, sql string }{
		{"node_inventory_reports", `WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (
				PARTITION BY node_id ORDER BY reported_at DESC, revision DESC, id DESC
			) AS rn FROM node_inventory_reports
		)
		DELETE FROM node_inventory_reports WHERE id IN (SELECT id FROM ranked WHERE rn > 1)`},
		{"node_control_sessions", `WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (
				PARTITION BY node_id ORDER BY connected_at DESC, id DESC
			) AS rn FROM node_control_sessions
		)
		DELETE FROM node_control_sessions WHERE id IN (SELECT id FROM ranked WHERE rn > 1)`},
	}
	for _, statement := range statements {
		exists, err := tableExists(ctx, tx, statement.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, statement.sql); err != nil {
			return err
		}
	}
	return nil
}
