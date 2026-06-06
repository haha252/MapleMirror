package files

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/node/localasset"
)

const downloadVerificationTTL = time.Minute

func (h *Handler) now() time.Time {
	return time.Now().UTC()
}

func (h *Handler) ensureDownloadAssetVerified(asset localAsset) error {
	if recentVerifiedAssetUnmodified(h.Storage, asset, h.now()) {
		return nil
	}
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

func recentVerifiedAssetUnmodified(storage string, asset localAsset, now time.Time) bool {
	if asset.VerifiedAt == "" {
		return false
	}
	verifiedAt, err := time.Parse(time.RFC3339Nano, asset.VerifiedAt)
	if err != nil {
		return false
	}
	if verifiedAt.After(now) || now.Sub(verifiedAt) >= downloadVerificationTTL {
		return false
	}
	clean, ok := localasset.CleanRelativePath(asset.RelativePath)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(storage, clean))
	if err != nil || info.IsDir() || info.Size() != asset.SizeBytes {
		return false
	}
	return !info.ModTime().After(verifiedAt)
}

func (h *Handler) updateLocalAssetState(assetID, state string) error {
	_, err := h.DB.Exec(`UPDATE local_assets SET state = ?,
		verified_at = ? WHERE asset_id = ?`,
		state, h.now().Format(time.RFC3339Nano), assetID)
	return err
}
