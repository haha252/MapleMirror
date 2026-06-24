package files

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"mirror-server/internal/downloadtoken"
)

func (h *Handler) serveAssetMetadata(w http.ResponseWriter, r *http.Request,
	claims downloadtoken.Claims, asset localAsset) {
	if err := h.ensureDownloadAssetVerified(asset); err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产状态不一致")
		return
	}
	path := filepath.Join(h.Storage, asset.RelativePath)
	file, err := os.Open(path)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产不可用")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != asset.SizeBytes {
		httpError(w, r, http.StatusNotFound, "本地资产状态不一致")
		return
	}
	w.Header().Set("X-Authorization-Request-ID", claims.RequestID)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": filepath.Base(asset.RelativePath)}))
	http.ServeContent(w, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
}
