package files

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (h *Handler) serveReplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, r, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	assetID, err := replicationAssetID(r)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "资产不存在")
		return
	}
	token := replicationBearer(r)
	claims, err := h.Signer.VerifyReplication(token)
	if err != nil || claims.SourceNodeID != h.NodeID || claims.AssetID != assetID || claims.TargetNodeID == "" {
		httpError(w, r, http.StatusUnauthorized, "复制令牌无效")
		return
	}
	if err := validateReplicationRange(r, claims.RangeStart, claims.RangeEnd); err != nil {
		httpError(w, r, http.StatusUnauthorized, "复制范围无效")
		return
	}
	if !h.claimReplicationToken(token) {
		httpError(w, r, http.StatusUnauthorized, "复制令牌已使用")
		return
	}
	asset, err := h.localAsset(assetID)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产不可用")
		return
	}
	if err := h.ensureReplicationAssetVerified(asset); err != nil {
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
	w.Header().Set("X-Replication-Task-ID", claims.TaskID)
	http.ServeContent(w, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
}

func validateReplicationRange(r *http.Request, start, end int64) error {
	if start == 0 && end == 0 {
		return nil
	}
	gotStart, gotEnd, err := parseSingleRange(r.Header.Get("Range"))
	if err != nil {
		return err
	}
	if gotStart != start || gotEnd != end {
		return fmt.Errorf("复制范围与令牌不一致")
	}
	return nil
}

func parseSingleRange(value string) (int64, int64, error) {
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, errors.New("复制范围格式不合法")
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, errors.New("复制范围格式不合法")
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	if start < 0 || end < start {
		return 0, 0, errors.New("复制范围格式不合法")
	}
	return start, end, nil
}

func replicationBearer(r *http.Request) string {
	const prefix = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return ""
}

func (h *Handler) claimReplicationToken(token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.replicationUse == nil {
		h.replicationUse = make(map[string]struct{})
	}
	if _, ok := h.replicationUse[token]; ok {
		return false
	}
	h.replicationUse[token] = struct{}{}
	return true
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
