package syncer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mirror-server/internal/protocol"
)

type Executor struct {
	DB      *sql.DB
	Storage string
	TempDir string
	Client  *http.Client
}

func (e Executor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	switch task.TaskType {
	case "asset_download":
		return e.download(ctx, task)
	case "asset_delete":
		return e.delete(task)
	case "inventory_reconcile":
		return protocol.SyncTaskResult{TaskID: task.TaskID, Result: "succeeded", Message: "库存对账任务已确认"}
	default:
		return protocol.SyncTaskResult{TaskID: task.TaskID, Result: "failed", Message: "未知同步任务类型"}
	}
}

func (e Executor) download(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	if err := e.recordTask(task, "running", ""); err != nil {
		return taskResult(task, "failed", "", 0, err.Error())
	}
	if err := os.MkdirAll(e.Storage, 0o755); err != nil {
		return taskResult(task, "temporary_error", "", 0, "创建存储目录失败")
	}
	if err := os.MkdirAll(e.TempDir, 0o755); err != nil {
		return taskResult(task, "temporary_error", "", 0, "创建临时目录失败")
	}
	tmpPath := filepath.Join(e.TempDir, task.TaskID+".tmp")
	digest, size, err := e.fetch(ctx, task.Asset.DownloadURL, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return taskResult(task, "temporary_error", digest, size, "下载资产失败")
	}
	if digest != task.Asset.DigestSHA256 {
		_ = os.Remove(tmpPath)
		return taskResult(task, "digest_mismatch", digest, size, "资产摘要不匹配")
	}
	if size != task.Asset.SizeBytes {
		_ = os.Remove(tmpPath)
		return taskResult(task, "size_mismatch", digest, size, "资产大小不匹配")
	}
	rel := safeName(task.Asset.AssetID, task.Asset.FileName)
	finalPath := filepath.Join(e.Storage, rel)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return taskResult(task, "temporary_error", digest, size, "资产原子落盘失败")
	}
	if err := e.upsertAsset(task.Asset.AssetID, rel, digest, size); err != nil {
		return taskResult(task, "temporary_error", digest, size, "写入本地库存失败")
	}
	_ = e.recordTask(task, "succeeded", "")
	return taskResult(task, "succeeded", digest, size, "资产已校验并落盘")
}

func (e Executor) fetch(ctx context.Context, url, tmpPath string) (string, int64, error) {
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("下载响应异常：%s", resp.Status)
	}
	file, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), resp.Body)
	if err != nil {
		return "", size, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (e Executor) delete(task protocol.SyncTask) protocol.SyncTaskResult {
	var rel string
	err := e.DB.QueryRow(`SELECT relative_path FROM local_assets WHERE asset_id = ?`,
		task.Asset.AssetID).Scan(&rel)
	if err != nil {
		return taskResult(task, "succeeded", "", 0, "本地资产不存在")
	}
	_ = os.Remove(filepath.Join(e.Storage, rel))
	_, err = e.DB.Exec(`UPDATE local_assets SET state = 'removed' WHERE asset_id = ?`,
		task.Asset.AssetID)
	if err != nil {
		return taskResult(task, "temporary_error", "", 0, "更新本地库存失败")
	}
	return taskResult(task, "succeeded", "", 0, "本地资产已清理")
}

func (e Executor) upsertAsset(assetID, rel, digest string, size int64) error {
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

func taskResult(task protocol.SyncTask, result, digest string, size int64, msg string) protocol.SyncTaskResult {
	return protocol.SyncTaskResult{TaskID: task.TaskID, AssetID: task.Asset.AssetID,
		Result: result, LocalDigestSHA256: digest, SizeBytes: size, Message: msg}
}

func safeName(assetID, name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "asset.bin"
	}
	return assetID + "-" + name
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
