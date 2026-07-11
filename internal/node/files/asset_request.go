package files

import (
	"errors"
	"net/http"
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
	return h.localAssetByPath(requested.RelativePath, claimAssetID)
}
