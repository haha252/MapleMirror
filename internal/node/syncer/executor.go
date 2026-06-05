package syncer

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/logging"
	"mirror-server/internal/protocol"
)

type Executor struct {
	DB      *sql.DB
	Storage string
	TempDir string
	Client  *http.Client
	Logger  *logging.Logger
	Probe   *SourceProbe
}

func (e Executor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点开始执行同步任务",
			slog.String("task_id", task.TaskID),
			slog.String("task_type", task.TaskType),
			slog.String("asset_id", task.Asset.AssetID))
	}
	switch task.TaskType {
	case "asset_download":
		return e.download(ctx, task)
	case "asset_delete":
		return e.delete(task)
	case "inventory_reconcile":
		return protocol.SyncTaskResult{
			TaskID:  task.TaskID,
			Result:  "succeeded",
			Message: "库存对账任务已确认",
		}
	default:
		return protocol.SyncTaskResult{
			TaskID:  task.TaskID,
			Result:  "failed",
			Message: "未知同步任务类型",
		}
	}
}

func (e Executor) download(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	if err := e.recordTask(task, "running", ""); err != nil {
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点记录下载任务开始失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "failed", "", 0, err.Error())
	}
	finish, err := beginAssetDownload(ctx, task.Asset.AssetID)
	if err != nil {
		return taskResult(task, "temporary_error", "", 0, "等待同资产下载完成失败")
	}
	defer finish()
	if result, ok := e.reuseVerifiedAsset(task); ok {
		_ = e.recordTask(task, "succeeded", result.Message)
		return result
	}
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点开始下载资产",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("download_url", task.Asset.DownloadURL),
			slog.String("digest", task.Asset.DigestSHA256),
			slog.Int64("size_bytes", task.Asset.SizeBytes))
	}
	if err := os.MkdirAll(e.Storage, 0o755); err != nil {
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点创建存储目录失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "temporary_error", "", 0, "创建存储目录失败")
	}
	tempDir := effectiveTempDir(e.Storage, e.TempDir)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点创建临时目录失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "temporary_error", "", 0, "创建临时目录失败")
	}
	tmpPath, err := tempAssetPath(tempDir, task.TaskID)
	if err != nil {
		return taskResult(task, "temporary_error", "", 0, "创建临时文件路径失败")
	}
	defer os.Remove(tmpPath)
	digest, size, err := e.fetchPrimary(ctx, task, tmpPath)
	if err != nil {
		if e.Logger != nil {
			message := "节点下载资产失败"
			if len(task.FallbackSources) > 0 {
				message = "节点源站下载失败，准备尝试其他节点复制"
			}
			e.Logger.Warn(context.Background(), message,
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.Int("fallback_sources", len(task.FallbackSources)),
				slog.String("error", err.Error()))
		}
		digest, size, err = e.fetchFallback(ctx, task, tmpPath)
		if err != nil {
			if e.Logger != nil && len(task.FallbackSources) == 0 {
				e.Logger.Warn(context.Background(), "节点源站不可用且没有可用节点副本，等待主节点重试",
					slog.String("task_id", task.TaskID),
					slog.String("asset_id", task.Asset.AssetID))
			}
			_ = os.Remove(tmpPath)
			return taskResult(task, "temporary_error", digest, size, "下载资产失败")
		}
	}
	if digest != task.Asset.DigestSHA256 {
		_ = os.Remove(tmpPath)
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点资产摘要不匹配",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("expected_digest", task.Asset.DigestSHA256),
				slog.String("actual_digest", digest))
		}
		return taskResult(task, "digest_mismatch", digest, size, "资产摘要不匹配")
	}
	if size != task.Asset.SizeBytes {
		_ = os.Remove(tmpPath)
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点资产大小不匹配",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.Int64("expected_size_bytes", task.Asset.SizeBytes),
				slog.Int64("actual_size_bytes", size))
		}
		return taskResult(task, "size_mismatch", digest, size, "资产大小不匹配")
	}
	rel := relativeAssetPath(task.Asset)
	finalPath := filepath.Join(e.Storage, rel)
	result := e.commitAsset(task, tmpPath, finalPath, rel, digest, size)
	if result.Result != "succeeded" {
		return result
	}
	_ = e.recordTask(task, "succeeded", result.Message)
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点资产下载完成",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("relative_path", rel),
			slog.String("digest", digest),
			slog.Int64("size_bytes", size))
	}
	return result
}

func (e Executor) reuseVerifiedAsset(task protocol.SyncTask) (protocol.SyncTaskResult, bool) {
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

func (e Executor) delete(task protocol.SyncTask) protocol.SyncTaskResult {
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点开始删除本地资产",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID))
	}
	var rel string
	err := e.DB.QueryRow(`SELECT relative_path FROM local_assets WHERE asset_id = ?`, task.Asset.AssetID).Scan(&rel)
	if err != nil {
		if e.Logger != nil {
			e.Logger.Debug(context.Background(), "节点删除任务对应资产不存在",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID))
		}
		return taskResult(task, "succeeded", "", 0, "本地资产不存在")
	}
	_ = os.Remove(filepath.Join(e.Storage, rel))
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
	if e.Logger != nil {
		e.Logger.Debug(context.Background(), "节点本地资产删除完成",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID))
	}
	return taskResult(task, "succeeded", "", 0, "本地资产已清理")
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
