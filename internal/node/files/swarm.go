package files

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (h *Handler) serveSwarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, r, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	assetID := strings.TrimPrefix(r.URL.Path, "/internal/swarm/")
	if assetID == "" || strings.Contains(assetID, "/") {
		httpError(w, r, http.StatusNotFound, "资产不存在")
		return
	}
	claims, err := h.Signer.VerifySwarm(replicationBearer(r))
	if err != nil || claims.SourceNodeID != h.NodeID || claims.TargetNodeID == "" ||
		claims.AssetID != assetID || claims.ManifestID == "" || claims.AssetSize <= 0 ||
		claims.PieceSize < swarm.MinPieceSize || claims.PieceSize > swarm.ProtocolMaxPieceSize ||
		(claims.PieceSize&(claims.PieceSize-1)) != 0 || claims.PieceCount <= 0 ||
		claims.PieceCount > int(swarm.MaxPieces) ||
		int((claims.AssetSize+claims.PieceSize-1)/claims.PieceSize) != claims.PieceCount {
		httpError(w, r, http.StatusUnauthorized, "Swarm capability 无效")
		return
	}
	start, endInclusive, err := parseSingleRange(r.Header.Get("Range"))
	if err != nil || endInclusive >= claims.AssetSize {
		httpError(w, r, http.StatusRequestedRangeNotSatisfiable, "Swarm Range 无效")
		return
	}
	pieceIndex := int(start / claims.PieceSize)
	pieceStart, pieceEnd, ok := swarm.PieceBounds(claims.AssetSize, claims.PieceSize, pieceIndex)
	if !ok || start < pieceStart || endInclusive >= pieceEnd {
		httpError(w, r, http.StatusRequestedRangeNotSatisfiable, "Range 必须位于单个 verified piece 内")
		return
	}

	path, err := h.swarmReadablePath(assetID, claims.ManifestID, claims.AssetSize,
		claims.PieceSize, claims.PieceCount, pieceIndex)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "Swarm piece 不可用")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "Swarm piece 不可用")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != claims.AssetSize {
		httpError(w, r, http.StatusNotFound, "Swarm 文件状态不一致")
		return
	}

	finish := func() {}
	if h.Activity != nil {
		finish = h.Activity.BeginSwarmUpload()
	}
	defer finish()

	length := endInclusive - start + 1
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, endInclusive, claims.AssetSize))
	w.Header().Set("X-Swarm-Manifest-ID", claims.ManifestID)
	w.Header().Set("X-Swarm-Piece", strconv.Itoa(pieceIndex))
	w.WriteHeader(http.StatusPartialContent)

	var target http.ResponseWriter = h.rateLimitedResponseWriter(r, w)
	if h.SwarmLimiter != nil {
		target = rateLimitedResponseWriter{ResponseWriter: target,
			writer: h.SwarmLimiter.WrapWriter(r.Context(), target)}
	}
	_, _ = io.CopyN(target, io.NewSectionReader(file, start, length), length)
}

func (h *Handler) swarmReadablePath(assetID, manifestID string, assetSize, pieceSize int64,
	pieceCount, pieceIndex int) (string, error) {
	if asset, err := h.localAsset(assetID); err == nil {
		if asset.SizeBytes != assetSize {
			return "", fmt.Errorf("committed asset size mismatch")
		}
		if err := h.ensureReplicationAssetVerified(asset); err != nil {
			return "", err
		}
		return filepath.Join(h.Storage, asset.RelativePath), nil
	}
	if h.Swarm == nil {
		return "", fmt.Errorf("partial registry unavailable")
	}
	manifest, ok := h.Swarm.Manifest(manifestID)
	if !ok || manifest.AssetID != assetID || manifest.AssetSize != assetSize ||
		manifest.PieceSize != pieceSize || manifest.PieceCount != pieceCount {
		return "", fmt.Errorf("manifest unavailable")
	}
	partial, ok := h.Swarm.Partial(assetID, manifestID)
	if !ok || !swarm.Has(partial.Bitset, pieceIndex) {
		return "", fmt.Errorf("piece unavailable")
	}
	if swarm.Has(partial.Trusted, pieceIndex) {
		return partial.Path, nil
	}
	if err := verifyPartialPiece(partial.Path, manifest, pieceIndex); err != nil {
		h.Swarm.ClearPiece(assetID, manifestID, pieceIndex)
		if current, ok := h.Swarm.Partial(assetID, manifestID); ok && h.DB != nil {
			_, _ = h.DB.Exec(`UPDATE swarm_partials SET verified_bitmap=?,updated_at=?,last_access_at=?
				WHERE asset_id=? AND manifest_id=?`, current.Bitset,
				time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano),
				assetID, manifestID)
		}
		return "", err
	}
	h.Swarm.MarkTrusted(assetID, manifestID, pieceIndex, true)
	return partial.Path, nil
}

func verifyPartialPiece(path string, manifest protocolv2.SwarmManifest, pieceIndex int) error {
	start, end, ok := swarm.PieceBounds(manifest.AssetSize, manifest.PieceSize, pieceIndex)
	if !ok || pieceIndex*32+32 > len(manifest.PieceHashes) {
		return fmt.Errorf("piece metadata invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != manifest.AssetSize {
		return fmt.Errorf("partial file size mismatch")
	}
	h := sha256.New()
	if _, err := io.CopyN(h, io.NewSectionReader(f, start, end-start), end-start); err != nil {
		return err
	}
	want := manifest.PieceHashes[pieceIndex*32 : (pieceIndex+1)*32]
	if !bytes.Equal(h.Sum(nil), want) {
		return fmt.Errorf("piece %d hash mismatch", pieceIndex)
	}
	return nil
}
