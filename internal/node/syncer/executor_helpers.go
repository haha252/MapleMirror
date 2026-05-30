package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"mirror-server/internal/protocol"
)

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

func safeName(assetID, name string) string {
	assetID = sanitizeNamePart(assetID)
	name = sanitizeNamePart(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "asset.bin"
	}
	return assetID + "-" + name
}

func sanitizeNamePart(value string) string {
	value = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 32 {
			return '_'
		}
		return r
	}, value)
	value = strings.TrimRight(value, ". ")
	if value == "" {
		return "asset"
	}
	return value
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
