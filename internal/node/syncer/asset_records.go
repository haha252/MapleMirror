package syncer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mirror-server/internal/protocol"
)

func (e Executor) reuseVerifiedAsset(task protocol.SyncTask) (protocol.SyncTaskResult, bool) {
	if result, ok := e.reuseVerifiedAssetRecord(task); ok {
		return result, true
	}
	return e.reuseExistingAssetFile(task)
}

func (e Executor) reuseVerifiedAssetRecord(task protocol.SyncTask) (protocol.SyncTaskResult, bool) {
	var rel, digest string
	var size int64
	err := e.DB.QueryRow(`SELECT relative_path, digest_sha256, size_bytes FROM local_assets
		WHERE asset_id = ? AND state = 'verified'`, task.Asset.AssetID).Scan(&rel, &digest, &size)
	if err != nil {
		return protocol.SyncTaskResult{}, false
	}
	if digest != task.Asset.DigestSHA256 || size != task.Asset.SizeBytes {
		return protocol.SyncTaskResult{}, false
	}
	if gotDigest, gotSize, err := fileDigest(filepath.Join(e.Storage, rel)); err != nil ||
		gotDigest != digest || gotSize != size {
		return protocol.SyncTaskResult{}, false
	}
	return taskResult(task, "succeeded", digest, size, "资产已由并发任务落盘"), true
}

func (e Executor) reuseExistingAssetFile(task protocol.SyncTask) (protocol.SyncTaskResult, bool) {
	rel := e.assetRelativePath(task.Asset)
	digest, size, err := fileDigest(filepath.Join(e.Storage, rel))
	if err != nil || digest != task.Asset.DigestSHA256 || size != task.Asset.SizeBytes {
		return protocol.SyncTaskResult{}, false
	}
	if err := e.upsertAsset(task.Asset.AssetID, rel, digest, size); err != nil {
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点恢复本地资产库存失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "temporary_error", digest, size, "恢复本地资产库存失败"), true
	}
	return taskResult(task, "succeeded", digest, size, "本地资产文件已存在并通过校验"), true
}

func (e Executor) delete(task protocol.SyncTask) protocol.SyncTaskResult {
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点开始删除本地资产",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID))
	}
	var rel, state string
	err := e.DB.QueryRow(`SELECT relative_path, state FROM local_assets WHERE asset_id = ?`, task.Asset.AssetID).Scan(&rel, &state)
	if err != nil {
		if e.Logger != nil {
			e.Logger.Debug(context.Background(), "节点删除任务对应资产不存在",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID))
		}
		return taskResult(task, "succeeded", "", 0, "本地资产不存在")
	}
	if state != "verified" {
		if _, err := e.DB.Exec(`UPDATE local_assets SET state = 'removed' WHERE asset_id = ?`, task.Asset.AssetID); err != nil {
			return taskResult(task, "temporary_error", "", 0, "更新本地状态失败")
		}
		return taskResult(task, "succeeded", "", 0, "本地资产已被替换，无需删除共用文件")
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == "." || relEscapes(clean) {
		return taskResult(task, "temporary_error", "", 0, "本地资产路径不安全")
	}
	var shared int
	if err := e.DB.QueryRow(`SELECT COUNT(*) FROM local_assets
		WHERE relative_path = ? AND asset_id != ? AND state = 'verified'`,
		rel, task.Asset.AssetID).Scan(&shared); err != nil {
		return taskResult(task, "temporary_error", "", 0, "检查本地资产路径失败")
	}
	if shared == 0 {
		if err := os.Remove(filepath.Join(e.Storage, clean)); err != nil && !os.IsNotExist(err) {
			if e.Logger != nil {
				e.Logger.Warn(context.Background(), "节点删除本地资产文件失败",
					slog.String("task_id", task.TaskID),
					slog.String("asset_id", task.Asset.AssetID),
					slog.String("error", err.Error()))
			}
			return taskResult(task, "temporary_error", "", 0, "删除本地资产文件失败")
		}
	}
	_, err = e.DB.Exec(`UPDATE local_assets SET state = 'removed' WHERE asset_id = ?`, task.Asset.AssetID)
	if err != nil {
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点更新本地删除状态失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "temporary_error", "", 0, "更新本地状态失败")
	}
	if shared == 0 {
		if err := removeEmptyAssetDirectories(e.Storage, clean); err != nil && e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点清理空资产目录失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("relative_path", clean),
				slog.String("error", err.Error()))
		}
	}
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点本地资产删除完成",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID))
	}
	return taskResult(task, "succeeded", "", 0, "本地资产已清理")
}

func relEscapes(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(os.PathSeparator))
}

func (e Executor) upsertAsset(assetID, rel, digest string, size int64) error {
	if _, err := e.DB.Exec(`UPDATE local_assets SET state = 'superseded',
		verified_at = ? WHERE relative_path = ? AND asset_id != ?
		AND state = 'verified'`,
		time.Now().UTC().Format(time.RFC3339Nano), rel, assetID); err != nil {
		return err
	}
	_, err := e.DB.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, ?, 'verified')
		ON CONFLICT(asset_id) DO UPDATE SET relative_path = excluded.relative_path,
		digest_sha256 = excluded.digest_sha256, size_bytes = excluded.size_bytes,
		verified_at = excluded.verified_at, state = 'verified'`,
		assetID, rel, digest, size, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (e Executor) recordTask(task protocol.SyncTask, state, message string) error {
	_, err := e.DB.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, error_message, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET state = excluded.state,
		error_message = excluded.error_message, updated_at = excluded.updated_at`,
		task.TaskID, task.Asset.AssetID, task.TaskType, state, nullable(message),
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
