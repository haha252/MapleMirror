package syncer

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"mirror-server/internal/protocol"
)

func (e Executor) commitAsset(task protocol.SyncTask, tmpPath, finalPath, rel, digest string, size int64) protocol.SyncTaskResult {
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		_ = os.Remove(tmpPath)
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点创建资产目录失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("path", filepath.Dir(finalPath)),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "temporary_error", digest, size, "创建资产目录失败")
	}
	if err := moveAssetFile(tmpPath, finalPath); err == nil {
		return e.recordCommittedAsset(task, rel, digest, size, "资产已校验并落盘")
	} else {
		return e.reuseOrReplaceTarget(task, tmpPath, finalPath, rel, digest, size, err)
	}
}

func (e Executor) reuseOrReplaceTarget(task protocol.SyncTask, tmpPath, finalPath, rel, digest string, size int64, renameErr error) protocol.SyncTaskResult {
	if existingDigest, existingSize, statErr := fileDigest(finalPath); statErr == nil &&
		existingDigest == digest && existingSize == size {
		_ = os.Remove(tmpPath)
		return e.recordCommittedAsset(task, rel, digest, size, "资产已由并发任务落盘")
	}
	if err := moveAssetFile(tmpPath, finalPath); err == nil {
		return e.recordCommittedAsset(task, rel, digest, size, "资产已校验并落盘")
	}
	_ = os.Remove(tmpPath)
	if e.Logger != nil {
		e.Logger.Warn(context.Background(), "节点落盘失败",
			slog.String("task_id", task.TaskID),
			slog.String("asset_id", task.Asset.AssetID),
			slog.String("path", finalPath),
			slog.String("error", renameErr.Error()))
	}
	return taskResult(task, "temporary_error", digest, size, "资产落盘失败")
}

func moveAssetFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	tmp, err := copyAssetToTargetDir(src, filepath.Dir(dst))
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyAssetToTargetDir(src, dir string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".asset-*")
	if err != nil {
		return "", err
	}
	tmp := out.Name()
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err = out.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

func (e Executor) recordCommittedAsset(task protocol.SyncTask, rel, digest string, size int64, msg string) protocol.SyncTaskResult {
	if err := e.upsertAsset(task.Asset.AssetID, rel, digest, size); err != nil {
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点写入本地库存失败",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("error", err.Error()))
		}
		return taskResult(task, "temporary_error", digest, size, "写入本地库存失败")
	}
	return taskResult(task, "succeeded", digest, size, msg)
}
