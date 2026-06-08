package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/protocol"
)

func bindTaskResultAsset(ctx context.Context, tx *sql.Tx, nodeID string,
	result protocol.SyncTaskResult) (protocol.SyncTaskResult, error) {
	var taskType, taskAssetID string
	err := tx.QueryRowContext(ctx, `SELECT task_type, COALESCE(asset_id, '')
		FROM node_tasks WHERE id = ? AND node_id = ?`, result.TaskID, nodeID).
		Scan(&taskType, &taskAssetID)
	if err != nil || !assetTaskType(taskType) {
		return result, err
	}
	if result.AssetID == taskAssetID {
		return result, nil
	}
	result.AssetID = taskAssetID
	result.Result = "temporary_error"
	result.LocalDigestSHA256 = ""
	result.SizeBytes = 0
	result.Message = "同步结果资产与任务不匹配"
	return result, nil
}

func assetTaskType(taskType string) bool {
	return taskType == "asset_download" || taskType == "asset_delete"
}
