package files

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (h *Handler) serveReplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpError(w, r, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	assetID, err := replicationAssetID(r)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "资产不存在")
		return
	}
	claims, err := h.Signer.VerifyReplication(bearer(r))
	if err != nil || claims.SourceNodeID != h.NodeID || claims.AssetID != assetID || claims.TargetNodeID == "" {
		httpError(w, r, http.StatusUnauthorized, "复制令牌无效")
		return
	}
	asset, err := h.localAsset(assetID)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产不可用")
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
	w.Header().Set("X-Replication-Task-ID", claims.TaskID)
	http.ServeContent(w, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
}

func replicationAssetID(r *http.Request) (string, error) {
	raw := strings.TrimPrefix(r.URL.EscapedPath(), "/internal/replication/")
	if raw == "" || strings.Contains(raw, "/") {
		return "", errors.New("资产路径不合法")
	}
	assetID, err := url.PathUnescape(raw)
	if err != nil || assetID == "" || strings.Contains(assetID, "/") {
		return "", errors.New("资产路径不合法")
	}
	return assetID, nil
}
