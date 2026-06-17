package files

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"mirror-server/internal/node/localasset"
)

const downloadVerificationTTL = 10 * time.Minute

func (h *Handler) now() time.Time {
	return time.Now().UTC()
}

func (h *Handler) ensureDownloadAssetVerified(asset localAsset) error {
	if h.downloadVerificationFresh(asset) {
		return nil
	}
	h.scheduleDownloadAssetVerification(asset)
	return nil
}

func (h *Handler) scheduleDownloadAssetVerification(asset localAsset) {
	h.mu.Lock()
	if h.verifyInFlight == nil {
		h.verifyInFlight = map[string]struct{}{}
	}
	if _, ok := h.verifyInFlight[asset.AssetID]; ok {
		h.mu.Unlock()
		return
	}
	h.verifyInFlight[asset.AssetID] = struct{}{}
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.verifyInFlight, asset.AssetID)
			h.mu.Unlock()
		}()
		if err := h.verifyDownloadAssetNow(asset); err != nil && h.Logger != nil {
			h.Logger.Warn(context.Background(), "下载文件后台校验失败",
				slog.String("asset_id", asset.AssetID),
				slog.String("error", err.Error()))
		}
	}()
}

func (h *Handler) verifyDownloadAssetNow(asset localAsset) error {
	state := localasset.Verify(h.Storage, localasset.Record{
		RelativePath: asset.RelativePath,
		DigestSHA256: asset.DigestSHA256,
		SizeBytes:    asset.SizeBytes,
	})
	return h.updateLocalAssetState(asset.AssetID, state)
}

func (h *Handler) ensureReplicationAssetVerified(asset localAsset) error {
	state := localasset.Verify(h.Storage, localasset.Record{
		RelativePath: asset.RelativePath,
		DigestSHA256: asset.DigestSHA256,
		SizeBytes:    asset.SizeBytes,
	})
	if err := h.updateLocalAssetState(asset.AssetID, state); err != nil {
		return err
	}
	if state != localasset.StateVerified {
		return errors.New("本地资产状态不一致")
	}
	return nil
}

func (h *Handler) downloadVerificationFresh(asset localAsset) bool {
	if asset.VerifiedAt == "" {
		return false
	}
	verifiedAt, err := time.Parse(time.RFC3339Nano, asset.VerifiedAt)
	if err != nil {
		return false
	}
	return h.now().Sub(verifiedAt) < downloadVerificationTTL
}

func (h *Handler) updateLocalAssetState(assetID, state string) error {
	_, err := h.DB.Exec(`UPDATE local_assets SET state = ?,
		verified_at = ? WHERE asset_id = ?`,
		state, h.now().Format(time.RFC3339Nano), assetID)
	return err
}
