package files

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
)

type Handler struct {
	DB      *sql.DB
	Storage string
	NodeID  string
	Signer  downloadtoken.Signer
	mu      sync.Mutex
	active  map[string]int
}

type localAsset struct {
	RelativePath string
	DigestSHA256 string
	SizeBytes    int64
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpError(w, r, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	assetID := strings.TrimPrefix(r.URL.Path, "/downloads/")
	if assetID == "" || strings.Contains(assetID, "/") {
		httpError(w, r, http.StatusNotFound, "资产不存在")
		return
	}
	claims, err := h.Signer.Verify(bearer(r))
	if err != nil || claims.AssetID != assetID || claims.NodeID != h.NodeID {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
		return
	}
	if claims.ClientPrefix != clientPrefix(r) {
		httpError(w, r, http.StatusForbidden, "客户端网络前缀不匹配")
		return
	}
	if !h.enter(claims.AuthorizationID, claims.RangeConcurrencyLimit) {
		httpError(w, r, http.StatusTooManyRequests, "Range 并发数超过授权限制")
		return
	}
	defer h.leave(claims.AuthorizationID)
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
	w.Header().Set("X-Authorization-Request-ID", claims.RequestID)
	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(asset.RelativePath))
	counter := &countingWriter{ResponseWriter: w}
	http.ServeContent(counter, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
	if counter.bytes > 0 {
		_ = h.recordTraffic(claims, assetID, requestid.FromContext(r.Context()), counter.bytes)
	}
}

func (h *Handler) localAsset(assetID string) (localAsset, error) {
	var out localAsset
	err := h.DB.QueryRow(`SELECT relative_path, digest_sha256, size_bytes FROM local_assets
		WHERE asset_id = ? AND state = 'verified'`, assetID).
		Scan(&out.RelativePath, &out.DigestSHA256, &out.SizeBytes)
	if err != nil {
		return out, err
	}
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
