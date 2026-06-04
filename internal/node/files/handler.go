package files

import (
	"database/sql"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"mirror-server/internal/assetpath"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	"mirror-server/internal/requestid"
)

type Handler struct {
	DB           *sql.DB
	Storage      string
	NodeID       string
	Signer       downloadtoken.Signer
	TrustedCIDRs []string
	Logger       *logging.Logger
	mu           sync.Mutex
	active       map[string]int
}

type localAsset struct {
	AssetID      string
	RelativePath string
	DigestSHA256 string
	SizeBytes    int64
}

type assetRequest struct {
	LegacyAssetID string
	RelativePath  string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/internal/replication/") {
		h.serveReplication(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpError(w, r, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	requested, err := parseAssetRequest(r)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "资产不存在")
		return
	}
	claims, err := h.Signer.Verify(bearer(r))
	if err != nil || claims.NodeID != h.NodeID {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
		return
	}
	asset, err := h.requestedAsset(requested)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产不可用")
		return
	}
	if claims.AssetID != asset.AssetID {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
		return
	}
	if claims.ClientPrefix != h.clientPrefix(r) {
		httpError(w, r, http.StatusForbidden, "客户端网络前缀不匹配")
		return
	}
	if h.Logger != nil {
		h.Logger.Info(r.Context(), "下载节点收到下载令牌",
			slog.String("request_id", requestid.FromContext(r.Context())),
			slog.String("authorization_id", claims.AuthorizationID),
			slog.String("asset_id", claims.AssetID),
			slog.String("client_ip", h.clientIP(r)),
			slog.String("project_id", claims.ProjectID),
			slog.String("system", claims.System),
			slog.String("architecture", claims.Architecture))
	}
	if !h.enter(claims.AuthorizationID, claims.RangeConcurrencyLimit) {
		httpError(w, r, http.StatusTooManyRequests, "Range 并发数超过授权限制")
		return
	}
	defer h.leave(claims.AuthorizationID)
	sent, err := h.authorizationBytes(claims.AuthorizationID)
	if err != nil || (r.Method != http.MethodHead && sent >= claims.MaxBytes) {
		httpError(w, r, http.StatusForbidden, "授权可发送字节数不足")
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
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment",
		map[string]string{"filename": filepath.Base(asset.RelativePath)}))
	counter := &countingWriter{ResponseWriter: w}
	http.ServeContent(counter, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
	if counter.bytes > 0 {
		_ = h.recordTraffic(claims, asset.AssetID, requestid.FromContext(r.Context()), counter.bytes)
	}
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

func (h *Handler) requestedAsset(requested assetRequest) (localAsset, error) {
	if requested.LegacyAssetID != "" {
		return h.localAsset(requested.LegacyAssetID)
	}
	return h.localAssetByPath(requested.RelativePath)
}

func (h *Handler) authorizationBytes(id string) (int64, error) {
	var sent int64
	err := h.DB.QueryRow(`SELECT COALESCE(SUM(sent_bytes), 0)
		FROM pending_traffic_events WHERE authorization_id = ?`, id).Scan(&sent)
	return sent, err
}

func (h *Handler) localAsset(assetID string) (localAsset, error) {
	var out localAsset
	err := h.DB.QueryRow(`SELECT asset_id, relative_path, digest_sha256, size_bytes FROM local_assets
		WHERE asset_id = ? AND state = 'verified'`, assetID).
		Scan(&out.AssetID, &out.RelativePath, &out.DigestSHA256, &out.SizeBytes)
	if err != nil {
		return out, err
	}
	return cleanLocalAsset(out)
}

func (h *Handler) localAssetByPath(rel string) (localAsset, error) {
	rows, err := h.DB.Query(`SELECT asset_id, relative_path, digest_sha256, size_bytes
		FROM local_assets WHERE relative_path = ? AND state = 'verified'`, rel)
	if err != nil {
		return localAsset{}, err
	}
	defer rows.Close()
	var out localAsset
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return localAsset{}, errors.New("本地资产路径不唯一")
		}
		if err := rows.Scan(&out.AssetID, &out.RelativePath, &out.DigestSHA256, &out.SizeBytes); err != nil {
			return localAsset{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return localAsset{}, err
	}
	if count != 1 {
		return localAsset{}, sql.ErrNoRows
	}
	return cleanLocalAsset(out)
}

func cleanLocalAsset(out localAsset) (localAsset, error) {
	clean := filepath.Clean(out.RelativePath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return out, errors.New("本地资产路径不安全")
	}
	out.RelativePath = clean
	return out, nil
}

func (h *Handler) enter(id string, limit int) bool {
	if limit <= 0 {
		limit = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		h.active = make(map[string]int)
	}
	if h.active[id] >= limit {
		return false
	}
	h.active[id]++
	return true
}

func (h *Handler) leave(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active[id] <= 1 {
		delete(h.active, id)
		return
	}
	h.active[id]--
}

func bearer(r *http.Request) string {
	const prefix = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return r.URL.Query().Get("token")
}

func httpError(w http.ResponseWriter, r *http.Request, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"status":"error","message":"` + message +
		`","request_id":"` + requestid.FromContext(r.Context()) + `"}`))
}

type countingWriter struct {
	http.ResponseWriter
	bytes int64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}
