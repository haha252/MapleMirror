package syncer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"mirror-server/internal/assetpath"
	"mirror-server/internal/protocol"
)

var errAssetTooLarge = errors.New("资产大小超过期望值")

func (e Executor) fetch(ctx context.Context, url, tmpPath string, maxSize int64) (string, int64, error) {
	return e.fetchWithToken(ctx, url, tmpPath, "", maxSize)
}

func (e Executor) fetchWithToken(ctx context.Context, url, tmpPath, token string, maxSize int64) (string, int64, error) {
	if err := validateSourceURL(url, e.AllowPrivateSourceURLs); err != nil {
		return "", 0, err
	}
	client := e.effectiveSourceClient()
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
	if maxSize >= 0 && resp.ContentLength > maxSize {
		return "", resp.ContentLength, errAssetTooLarge
	}
	file, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	body := e.rateLimitedBody(resp.Body)
	writer := &limitedAssetWriter{dst: io.MultiWriter(file, hash), max: maxSize}
	size, err := io.Copy(writer, body)
	if err != nil {
		if errors.Is(err, errAssetTooLarge) {
			return "", writer.reportedSize(), err
		}
		return "", size, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

type limitedAssetWriter struct {
	dst     io.Writer
	max     int64
	written int64
	tooBig  bool
}

func (w *limitedAssetWriter) Write(p []byte) (int, error) {
	if w.max < 0 {
		return w.writeAll(p)
	}
	remaining := w.max - w.written
	if remaining <= 0 {
		w.tooBig = true
		return 0, errAssetTooLarge
	}
	if int64(len(p)) > remaining {
		n, err := w.dst.Write(p[:remaining])
		w.written += int64(n)
		if err != nil {
			return n, err
		}
		w.tooBig = true
		return n, errAssetTooLarge
	}
	return w.writeAll(p)
}

func (w *limitedAssetWriter) writeAll(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *limitedAssetWriter) reportedSize() int64 {
	if w.tooBig {
		return w.max + 1
	}
	return w.written
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
	canonical := assetpath.SafeRelativePath(asset.ProjectID, asset.Version, asset.FileName)
	if asset.AssetID == "" {
		return canonical
	}
	sum := sha256.Sum256([]byte(asset.AssetID))
	return filepath.Join(filepath.Dir(canonical), ".mirror-assets",
		hex.EncodeToString(sum[:8]), filepath.Base(canonical))
}

func tempAssetPath(tempDir, taskID string) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return filepath.Join(tempDir, taskID+"-"+hex.EncodeToString(suffix[:])+".tmp"), nil
}

func effectiveTempDir(storageDir, tempDir string) string {
	storageAbs, storageErr := filepath.Abs(filepath.Clean(storageDir))
	tempAbs, tempErr := filepath.Abs(filepath.Clean(tempDir))
	if storageErr != nil || tempErr != nil {
		return tempDir
	}
	if sameOrInside(tempAbs, storageAbs) {
		return filepath.Join(filepath.Dir(storageAbs), "."+filepath.Base(storageAbs)+"-tmp")
	}
	return tempDir
}

func sameOrInside(path, parent string) bool {
	if path == parent {
		return true
	}
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", size, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
