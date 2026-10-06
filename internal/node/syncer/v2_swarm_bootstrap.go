package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (e Executor) executeV2Bootstrap(ctx context.Context, task protocolv2.SyncTask, legacy protocol.SyncTask) (protocolv2.SyncResult, *protocolv2.SwarmManifest) {
	if reused, ok := e.reuseVerifiedAsset(legacy); ok && reused.Result == "succeeded" {
		manifest, err := e.manifestFromCommittedFile(task)
		if err != nil {
			return protocolv2.SyncResult{TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: task.Asset.AssetID, Result: "temporary_error", Message: "重新生成 piece manifest 失败: " + err.Error()}, nil
		}
		return v2ResultFromLegacy(task, reused), manifest
	}
	if len(task.WholeSources) > 0 {
		if result, manifest, done := e.executeV2PeerBootstrap(ctx, task, legacy); done {
			return result, manifest
		}
	}
	if e.Capacity != nil {
		release, err := e.Capacity.ReserveDownload(task.Asset.SizeBytes)
		if err != nil {
			return v2Failure(task, "temporary_error", "磁盘可用空间不足"), nil
		}
		defer release()
	}
	if err := os.MkdirAll(e.Storage, 0o755); err != nil {
		return v2Failure(task, "temporary_error", "创建存储目录失败"), nil
	}
	tempDir := effectiveTempDir(e.Storage, e.TempDir)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return v2Failure(task, "temporary_error", "创建临时目录失败"), nil
	}
	tmpPath, err := tempAssetPath(tempDir, task.TaskID)
	if err != nil {
		return v2Failure(task, "temporary_error", err.Error()), nil
	}
	defer os.Remove(tmpPath)
	manifest, digest, size, err := e.fetchOriginWithManifest(ctx, task, tmpPath)
	if err != nil {
		return v2Failure(task, "temporary_error", "下载 seed 失败: "+err.Error()), nil
	}
	if digest != task.Asset.DigestSHA256 {
		return v2Result(task, "digest_mismatch", digest, size, "资产摘要不匹配"), nil
	}
	if size != task.Asset.SizeBytes {
		return v2Result(task, "size_mismatch", digest, size, "资产大小不匹配"), nil
	}
	rel := e.assetRelativePath(legacy.Asset)
	finalPath := filepath.Join(e.Storage, rel)
	result := e.commitAsset(legacy, tmpPath, finalPath, rel, digest, size)
	return v2ResultFromLegacy(task, result), manifest
}

func (e Executor) fetchOriginWithManifest(ctx context.Context, task protocolv2.SyncTask, tmpPath string) (*protocolv2.SwarmManifest, string, int64, error) {
	if e.ForcePeerDownload {
		return nil, "", 0, errPrimarySourceDisabled
	}
	pieceSize, pieceCount, err := swarm.ChoosePieceSize(task.Asset.SizeBytes)
	if err != nil {
		return nil, "", 0, err
	}
	if err := validateSourceURL(task.Asset.DownloadURL, e.AllowPrivateSourceURLs); err != nil {
		return nil, "", 0, err
	}
	client := e.effectiveSourceClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.Asset.DownloadURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", 0, fmt.Errorf("下载响应异常: %s", resp.Status)
	}
	if resp.ContentLength >= 0 && resp.ContentLength > task.Asset.SizeBytes {
		return nil, "", resp.ContentLength, errAssetTooLarge
	}
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, "", 0, err
	}
	defer f.Close()
	body := e.rateLimitedBody(resp.Body)
	whole := sha256.New()
	hashes := make([]byte, 0, pieceCount*32)
	var total int64
	for i := 0; i < pieceCount; i++ {
		start, end, _ := swarm.PieceBounds(task.Asset.SizeBytes, pieceSize, i)
		want := end - start
		ph := sha256.New()
		n, copyErr := io.CopyN(io.MultiWriter(f, whole, ph), body, want)
		total += n
		if copyErr != nil {
			return nil, "", total, copyErr
		}
		hashes = append(hashes, ph.Sum(nil)...)
	}
	var extra [1]byte
	if n, _ := body.Read(extra[:]); n > 0 {
		return nil, "", total + 1, errAssetTooLarge
	}
	digest := "sha256:" + hex.EncodeToString(whole.Sum(nil))
	m := &protocolv2.SwarmManifest{AssetID: task.Asset.AssetID, AssetSize: task.Asset.SizeBytes, AssetSHA256: digest, PieceLayoutVersion: swarm.LayoutVersion, PieceSize: pieceSize, PieceCount: pieceCount, PieceHashAlgorithm: "sha256", PieceHashes: hashes}
	m.ManifestID = swarm.ManifestID(m.AssetID, m.AssetSHA256, m.AssetSize, m.PieceSize, m.PieceHashes)
	return m, digest, total, nil
}

func (e Executor) manifestFromCommittedFile(task protocolv2.SyncTask) (*protocolv2.SwarmManifest, error) {
	legacy := legacyTaskFromV2(task)
	rel := e.assetRelativePath(legacy.Asset)
	path := filepath.Join(e.Storage, rel)
	pieceSize, pieceCount, err := swarm.ChoosePieceSize(task.Asset.SizeBytes)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	whole := sha256.New()
	hashes := make([]byte, 0, pieceCount*32)
	var total int64
	for i := 0; i < pieceCount; i++ {
		start, end, _ := swarm.PieceBounds(task.Asset.SizeBytes, pieceSize, i)
		ph := sha256.New()
		n, err := io.CopyN(io.MultiWriter(whole, ph), f, end-start)
		total += n
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, ph.Sum(nil)...)
	}
	digest := "sha256:" + hex.EncodeToString(whole.Sum(nil))
	if digest != task.Asset.DigestSHA256 || total != task.Asset.SizeBytes {
		return nil, errors.New("本地完整文件与权威摘要不一致")
	}
	m := &protocolv2.SwarmManifest{AssetID: task.Asset.AssetID, AssetSize: total, AssetSHA256: digest, PieceLayoutVersion: swarm.LayoutVersion, PieceSize: pieceSize, PieceCount: pieceCount, PieceHashAlgorithm: "sha256", PieceHashes: hashes}
	m.ManifestID = swarm.ManifestID(m.AssetID, m.AssetSHA256, m.AssetSize, m.PieceSize, m.PieceHashes)
	return m, nil
}
