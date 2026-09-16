package syncer

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/logging"
	"mirror-server/internal/node/capacity"
	"mirror-server/internal/node/swarmstate"
	"mirror-server/internal/protocol"
)

type Executor struct {
	DB                        *sql.DB
	Storage                   string
	TempDir                   string
	Client                    *http.Client
	SourceClient              *http.Client
	Logger                    *logging.Logger
	Probe                     *SourceProbe
	BandwidthLimitBPS         int64 // legacy/config value; SyncLimiter is authoritative when non-nil.
	SyncLimiter               *BandwidthLimiter
	ForcePeerDownload         bool
	PeerFallbackWorkers       int
	PeerFallbackMinSize       int64
	PeerFallbackMaxConcurrent int
	AllowPrivateSourceURLs    bool
	Capacity                  *capacity.Manager
	Swarm                     *swarmstate.Registry
}

const DefaultHTTPClientTimeout = 30 * time.Minute

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
		if err := e.forceNextInventoryReport(); err != nil {
			return protocol.SyncTaskResult{
				TaskID:  task.TaskID,
				Result:  "temporary_error",
				Message: "请求完整库存上报失败",
			}
		}
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
		_ = e.recordTask(task, result.Result, result.Message)
		return result
	}
	if e.Capacity != nil {
		release, err := e.Capacity.ReserveDownload(task.Asset.SizeBytes)
		if err != nil {
			return taskResult(task, "temporary_error", "", 0, "磁盘可用空间不足")
		}
		defer release()
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
	peerFallbackAttempted := false
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
		if len(task.FallbackSources) == 0 {
			_ = os.Remove(tmpPath)
			if errors.Is(err, errAssetTooLarge) {
				return taskResult(task, "size_mismatch", digest, size, "资产大小超过期望")
			}
			if e.Logger != nil {
				e.Logger.Warn(context.Background(), "节点源站不可用且没有可用节点副本，等待主节点重试",
					slog.String("task_id", task.TaskID),
					slog.String("asset_id", task.Asset.AssetID))
			}
			return taskResult(task, "temporary_error", digest, size, "下载资产失败")
		}
		digest, size, peerFallbackAttempted, err = e.fetchFallback(ctx, task, tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			if errors.Is(err, errAssetTooLarge) {
				return taskResultWithPeerFallback(task, "size_mismatch", digest, size, "资产大小超过期望", peerFallbackAttempted)
			}
			return taskResultWithPeerFallback(task, "temporary_error", digest, size, "下载资产失败", peerFallbackAttempted)
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
		return taskResultWithPeerFallback(task, "digest_mismatch", digest, size, "资产摘要不匹配", peerFallbackAttempted)
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
		return taskResultWithPeerFallback(task, "size_mismatch", digest, size, "资产大小不匹配", peerFallbackAttempted)
	}
	rel := relativeAssetPath(task.Asset)
	finalPath := filepath.Join(e.Storage, rel)
	result := e.commitAsset(task, tmpPath, finalPath, rel, digest, size)
	if peerFallbackAttempted {
		result.PeerFallbackAttempted = true
	}
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

func taskResultWithPeerFallback(task protocol.SyncTask, result, digest string, size int64, message string, peerFallbackAttempted bool) protocol.SyncTaskResult {
	out := taskResult(task, result, digest, size, message)
	out.PeerFallbackAttempted = peerFallbackAttempted
	return out
}
