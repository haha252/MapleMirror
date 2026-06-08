package files

import (
	"errors"
	"time"

	"mirror-server/internal/node/localasset"
)

func (h *Handler) now() time.Time {
	return time.Now().UTC()
}

func (h *Handler) ensureDownloadAssetVerified(asset localAsset) error {
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

func (h *Handler) updateLocalAssetState(assetID, state string) error {
	_, err := h.DB.Exec(`UPDATE local_assets SET state = ?,
		verified_at = ? WHERE asset_id = ?`,
		state, h.now().Format(time.RFC3339Nano), assetID)
	return err
}
