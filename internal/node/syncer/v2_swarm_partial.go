package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/node/swarmstate"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (e Executor) openSwarmPartial(task protocolv2.SyncTask, m protocolv2.SwarmManifest) (*os.File, []byte, error) {
	if err := e.removeOtherSwarmPartials(task.Asset.AssetID, m.ManifestID); err != nil {
		return nil, nil, err
	}
	root := filepath.Join(effectiveTempDir(e.Storage, e.TempDir), "swarm-partials")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256([]byte(task.Asset.AssetID + "\x00" + m.ManifestID))
	path := filepath.Join(root, hex.EncodeToString(sum[:])+".part")
	bits := make([]byte, swarm.BitsetBytes(m.PieceCount))
	var dbPath string
	var stored []byte
	if e.DB != nil {
		err := e.DB.QueryRow(`SELECT partial_path,verified_bitmap FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, task.Asset.AssetID, m.ManifestID).Scan(&dbPath, &stored)
		if err == nil && dbPath == path && len(stored) == len(bits) {
			copy(bits, stored)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, err
	}
	info, _ := f.Stat()
	if info == nil || info.Size() != m.AssetSize {
		if err := f.Truncate(m.AssetSize); err != nil {
			f.Close()
			return nil, nil, err
		}
		for i := range bits {
			bits[i] = 0
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if e.DB != nil {
		_, err = e.DB.Exec(`INSERT INTO swarm_partials(asset_id,manifest_id,partial_path,asset_size,piece_size,piece_count,verified_bitmap,created_at,updated_at,last_access_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(asset_id,manifest_id) DO UPDATE SET partial_path=excluded.partial_path,asset_size=excluded.asset_size,piece_size=excluded.piece_size,piece_count=excluded.piece_count,updated_at=excluded.updated_at,last_access_at=excluded.last_access_at`, task.Asset.AssetID, m.ManifestID, path, m.AssetSize, m.PieceSize, m.PieceCount, bits, now, now, now)
		if err != nil {
			f.Close()
			return nil, nil, err
		}
	}
	if e.Swarm != nil {
		e.Swarm.SetManifest(m)
		e.Swarm.SetPartial(swarmstate.Partial{AssetID: m.AssetID, ManifestID: m.ManifestID, Path: path, Bitset: bits, Trusted: make([]byte, len(bits))})
	}
	return f, bits, nil
}

func (e Executor) partialBitmapFlusher(ctx context.Context, task protocolv2.SyncTask, m protocolv2.SwarmManifest, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if e.Swarm != nil {
				if p, ok := e.Swarm.Partial(m.AssetID, m.ManifestID); ok {
					_ = e.persistPartialBitmap(m.AssetID, m.ManifestID, p.Bitset)
				}
			}
			return
		case <-ticker.C:
			if e.Swarm != nil {
				if p, ok := e.Swarm.Partial(m.AssetID, m.ManifestID); ok {
					_ = e.persistPartialBitmap(m.AssetID, m.ManifestID, p.Bitset)
				}
			}
		}
	}
}
func (e Executor) persistPartialBitmap(asset, manifest string, bits []byte) error {
	if e.DB == nil {
		return nil
	}
	_, err := e.DB.Exec(`UPDATE swarm_partials SET verified_bitmap=?,updated_at=?,last_access_at=? WHERE asset_id=? AND manifest_id=?`, bits, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), asset, manifest)
	return err
}

func (e Executor) finishSwarmPartial(task protocolv2.SyncTask, legacy protocol.SyncTask, m protocolv2.SwarmManifest, path string, bits []byte) (protocolv2.SyncResult, *protocolv2.SwarmManifest) {
	if !swarm.Complete(bits, m.PieceCount) {
		return v2Failure(task, "temporary_error", "partial incomplete"), nil
	}
	digest, size, err := fileDigest(path)
	if err != nil {
		return v2Failure(task, "temporary_error", err.Error()), nil
	}
	if digest != m.AssetSHA256 || size != m.AssetSize {
		cleared, err := e.revalidateMarkedPartialPieces(m, path, bits)
		if err == nil && cleared > 0 {
			_ = e.persistPartialBitmap(m.AssetID, m.ManifestID, bits)
			if e.Swarm != nil {
				e.Swarm.UpdateBitset(m.AssetID, m.ManifestID, bits)
			}
			return v2Failure(task, "temporary_error", fmt.Sprintf("partial integrity recovery cleared %d corrupted pieces", cleared)), nil
		}
		return v2Result(task, "digest_mismatch", digest, size, "最终整文件校验失败"), nil
	}
	rel := e.assetRelativePath(legacy.Asset)
	res := e.commitAsset(legacy, path, filepath.Join(e.Storage, rel), rel, digest, size)
	if res.Result == "succeeded" {
		if e.DB != nil {
			_, _ = e.DB.Exec(`DELETE FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, m.AssetID, m.ManifestID)
		}
		if e.Swarm != nil {
			e.Swarm.RemovePartial(m.AssetID, m.ManifestID)
		}
	}
	if res.Result == "succeeded" {
		return v2ResultFromLegacy(task, res), &m
	}
	return v2ResultFromLegacy(task, res), nil
}

func (e Executor) fullOriginFallbackV2(ctx context.Context, task protocolv2.SyncTask, legacy protocol.SyncTask, m protocolv2.SwarmManifest, cause error) (protocolv2.SyncResult, *protocolv2.SwarmManifest) {
	if e.ForcePeerDownload {
		return v2Failure(task, "temporary_error", "swarm peers unavailable and origin is disabled: "+cause.Error()), nil
	}
	tmp, err := tempAssetPath(effectiveTempDir(e.Storage, e.TempDir), task.TaskID+"-origin")
	if err != nil {
		return v2Failure(task, "temporary_error", cause.Error()), nil
	}
	defer os.Remove(tmp)
	crossCheck, digest, size, err := e.fetchOriginWithManifest(ctx, task, tmp)
	if err != nil {
		return v2Failure(task, "temporary_error", fmt.Sprintf("swarm failed: %v; origin fallback: %v", cause, err)), nil
	}
	if digest != m.AssetSHA256 || size != m.AssetSize {
		return v2Result(task, "digest_mismatch", digest, size, "origin fallback integrity mismatch"), nil
	}
	rel := e.assetRelativePath(legacy.Asset)
	res := e.commitAsset(legacy, tmp, filepath.Join(e.Storage, rel), rel, digest, size)
	if res.Result == "succeeded" {
		e.removeSwarmPartial(m.AssetID, m.ManifestID)
		return v2ResultFromLegacy(task, res), crossCheck
	}
	return v2ResultFromLegacy(task, res), nil
}

func v2Failure(task protocolv2.SyncTask, result, message string) protocolv2.SyncResult {
	return protocolv2.SyncResult{TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: task.Asset.AssetID, Result: result, Message: message}
}
func v2Result(task protocolv2.SyncTask, result, digest string, size int64, message string) protocolv2.SyncResult {
	r := v2Failure(task, result, message)
	r.LocalDigestSHA256 = digest
	r.SizeBytes = size
	return r
}

func (e Executor) revalidateMarkedPartialPieces(m protocolv2.SwarmManifest, path string, bits []byte) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	cleared := 0
	for i := 0; i < m.PieceCount; i++ {
		if !swarm.Has(bits, i) {
			continue
		}
		start, end, ok := swarm.PieceBounds(m.AssetSize, m.PieceSize, i)
		if !ok || (i+1)*32 > len(m.PieceHashes) {
			return cleared, errors.New("invalid manifest piece metadata")
		}
		h := sha256.New()
		if _, err := io.CopyN(h, io.NewSectionReader(f, start, end-start), end-start); err != nil {
			return cleared, err
		}
		if !bytes.Equal(h.Sum(nil), m.PieceHashes[i*32:(i+1)*32]) {
			swarm.Clear(bits, i)
			cleared++
			if e.Swarm != nil {
				e.Swarm.ClearPiece(m.AssetID, m.ManifestID, i)
			}
		}
	}
	return cleared, nil
}

func (e Executor) removeSwarmPartial(assetID, manifestID string) {
	var path string
	if e.DB != nil {
		_ = e.DB.QueryRow(`SELECT partial_path FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, assetID, manifestID).Scan(&path)
		_, _ = e.DB.Exec(`DELETE FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, assetID, manifestID)
	}
	if e.Swarm != nil {
		e.Swarm.RemovePartial(assetID, manifestID)
	}
	if path != "" {
		_ = os.Remove(path)
	}
}
