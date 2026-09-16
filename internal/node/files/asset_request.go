package files

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"mirror-server/internal/assetpath"
)

type assetRequest struct {
	LegacyAssetID string
	RelativePath  string
}

func parseAssetRequest(r *http.Request) (assetRequest, error) {
	if strings.HasPrefix(r.URL.Path, "/downloads/") {
		assetID := strings.TrimPrefix(r.URL.Path, "/downloads/")
		if assetID == "" || strings.Contains(assetID, "/") {
			return assetRequest{}, errors.New("资产路径不合法")
		}
		return assetRequest{LegacyAssetID: assetID}, nil
	}
	parts, err := assetpath.ParsePublicPath(r.URL.EscapedPath())
	if err != nil {
		return assetRequest{}, err
	}
	return assetRequest{RelativePath: assetpath.SafeRelativePath(parts.ProjectID, parts.Version, parts.FileName)}, nil
}

func (h *Handler) requestedAsset(requested assetRequest, claimAssetID string) (localAsset, error) {
	if requested.LegacyAssetID != "" {
		return h.localAsset(requested.LegacyAssetID)
	}
	// The authorization binds the request to an exact asset ID. Modern assets may
	// use an identity-isolated physical path so multiple generations of a mutable
	// tag can coexist, but the requested public path must still match that asset.
	asset, err := h.localAsset(claimAssetID)
	if err != nil {
		return localAsset{}, err
	}
	if !matchesPublicAssetPath(asset.RelativePath, requested.RelativePath) {
		return localAsset{}, sql.ErrNoRows
	}
	return asset, nil
}

func matchesPublicAssetPath(localRel, publicRel string) bool {
	local := filepath.Clean(localRel)
	public := filepath.Clean(publicRel)
	if local == public {
		return true
	}
	publicDir, publicFile := filepath.Dir(public), filepath.Base(public)
	localDir := filepath.Dir(local)
	if filepath.Base(local) != publicFile {
		return false
	}
	identityDir := filepath.Base(localDir)
	markerDir := filepath.Dir(localDir)
	return identityDir != "." && identityDir != "" &&
		filepath.Base(markerDir) == ".mirror-assets" &&
		filepath.Dir(markerDir) == publicDir
}
