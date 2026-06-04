package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"

	"mirror-server/internal/assetpath"
	"mirror-server/internal/protocol"
)

func (e Executor) fetch(ctx context.Context, url, tmpPath string) (string, int64, error) {
	return e.fetchWithToken(ctx, url, tmpPath, "")
}

func (e Executor) fetchWithToken(ctx context.Context, url, tmpPath, token string) (string, int64, error) {
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("下载响应异常: %s", resp.Status)
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

func taskResult(task protocol.SyncTask, result, digest string, size int64, msg string) protocol.SyncTaskResult {
	return protocol.SyncTaskResult{
		TaskID:            task.TaskID,
		AssetID:           task.Asset.AssetID,
		Result:            result,
		LocalDigestSHA256: digest,
		SizeBytes:         size,
		Message:           msg,
	}
}

func relativeAssetPath(asset protocol.SyncAsset) string {
	if asset.ProjectID == "" || asset.Version == "" {
		return assetpath.SafeRelativePath(asset.AssetID, "legacy", asset.FileName)
	}
	return assetpath.SafeRelativePath(asset.ProjectID, asset.Version, asset.FileName)
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
