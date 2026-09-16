package syncer

import (
	"path/filepath"

	"mirror-server/internal/protocol"
)

// assetRelativePath keeps an existing local asset on its recorded physical path.
// New assets fall back to the current relativeAssetPath policy.
func (e Executor) assetRelativePath(asset protocol.SyncAsset) string {
	if e.DB != nil && asset.AssetID != "" {
		var rel string
		if err := e.DB.QueryRow(`SELECT relative_path FROM local_assets WHERE asset_id = ?`, asset.AssetID).Scan(&rel); err == nil {
			clean := filepath.Clean(rel)
			if !filepath.IsAbs(clean) && clean != "." && !relEscapes(clean) {
				return clean
			}
		}
	}
	return relativeAssetPath(asset)
}
