package control

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) sendPendingTaskResults(conn net.Conn, reqID string, sequence uint64) (uint64, error) {
	if c.DB == nil {
		return sequence, nil
	}
	rows, err := c.DB.Query(`SELECT task_id, asset_id, result, COALESCE(local_digest_sha256, ''),
		size_bytes, COALESCE(message, '') FROM pending_sync_task_results
		WHERE reported_at IS NULL ORDER BY created_at LIMIT 20`)
	if err != nil {
		return sequence, err
	}
	defer rows.Close()
	var results []protocol.SyncTaskResult
	for rows.Next() {
		result, err := scanPendingTaskResult(rows)
		if err != nil {
			return sequence, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return sequence, err
	}
	for _, result := range results {
		if err := c.sendTaskResult(conn, reqID, sequence, result); err != nil {
			return sequence, err
		}
		_, err = c.DB.Exec(`UPDATE pending_sync_task_results SET reported_at = ?
			WHERE task_id = ?`, time.Now().UTC().Format(time.RFC3339Nano), result.TaskID)
		if err != nil {
			return sequence, err
		}
		sequence++
	}
	return sequence, nil
}

func scanPendingTaskResult(rows *sql.Rows) (protocol.SyncTaskResult, error) {
	var result protocol.SyncTaskResult
	err := rows.Scan(&result.TaskID, &result.AssetID, &result.Result,
		&result.LocalDigestSHA256, &result.SizeBytes, &result.Message)
	return result, err
}

func (c Client) executeTaskAsync(task protocol.SyncTask) {
	go func() {
		result := c.Executor.Execute(context.Background(), task)
		if c.Logger != nil {
			c.Logger.Debug(context.Background(), "节点完成同步任务执行",
				slog.String("node_id", c.NodeID),
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("result", result.Result),
				slog.Int64("size_bytes", result.SizeBytes))
		}
		if err := c.storePendingTaskResult(result); err != nil && c.Logger != nil {
			c.Logger.Warn(context.Background(), "节点保存待上报同步结果失败",
				slog.String("node_id", c.NodeID),
				slog.String("task_id", result.TaskID),
				slog.String("asset_id", result.AssetID),
				slog.String("error", err.Error()))
		}
	}()
}

func (c Client) storePendingTaskResult(result protocol.SyncTaskResult) error {
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(task_id) DO UPDATE SET asset_id = excluded.asset_id,
		result = excluded.result, local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, message = excluded.message,
		created_at = excluded.created_at, reported_at = NULL`,
		result.TaskID, result.AssetID, result.Result, nullableString(result.LocalDigestSHA256),
		result.SizeBytes, nullableString(result.Message), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
