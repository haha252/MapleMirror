package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type Handler struct {
	DB              *sql.DB
	Storage, NodeID string
	Signer          downloadtoken.Signer
	TrustedCIDRs    []string
	Logger          *logging.Logger
	TrafficLimiter  trafficLimiter
	ProbeStore      interface {
		Response(string) (protocol.PublicProbeResponse, bool)
	}
	mu             sync.Mutex
	active         map[string]int
	replicationUse map[string]struct{}
	verifyInFlight map[string]struct{}
	budgets        map[string]int64
	pendingTraffic map[string][]pendingTrafficEvent
	downloadLogs   map[string]struct{}
}

type localAsset struct {
	AssetID      string
	RelativePath string
	DigestSHA256 string
	SizeBytes    int64
	VerifiedAt   string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/.well-known/mirror-node/probes/") {
		h.servePublicProbe(w, r)
		return
	}
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
	claims, err := h.verifyDownloadToken(bearer(r))
	if err != nil {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	asset, err := h.requestedAsset(requested, claims.AssetID)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产不可用")
		return
	}
	if claims.AssetID != asset.AssetID {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
		return
	}
	if r.Method == http.MethodHead {
		h.serveAssetMetadata(w, r, claims, asset)
		return
	}
	timing, err := h.beginAuthorization(claims)
	if err != nil {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
		return
	}
	h.logDownloadRequestOnce(r, claims)
	if !h.enter(claims.AuthorizationID, claims.RangeConcurrencyLimit) {
		httpError(w, r, http.StatusTooManyRequests, "Range 并发数超过授权限制")
		return
	}
	defer h.leave(claims.AuthorizationID)
	if h.rejectPendingTraffic(w, r, claims.AuthorizationID) {
		return
	}
	sent, err := h.authorizationBytes(claims.AuthorizationID)
	limit := h.authorizationLimit(claims)
	if err != nil || sent >= limit {
		httpError(w, r, http.StatusForbidden, "授权可发送字节数不足")
		return
	}
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
	target := h.rateLimitedResponseWriter(r, w)
	counter := &limitCountingWriter{ResponseWriter: target, handler: h,
		authorizationID: claims.AuthorizationID, claims: claims, limit: limit,
		sent: sent, timing: timing, assetID: asset.AssetID,
		nodeRequestID: requestid.FromContext(r.Context()), lastCheckpointAt: time.Now().UTC(),
		requestContext: r.Context()}
	http.ServeContent(counter, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
	counter.flushTrafficCheckpoint(true)
}

func (h *Handler) servePublicProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, r, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	challengeID := strings.TrimPrefix(r.URL.Path,
		"/.well-known/mirror-node/probes/")
	if challengeID == "" || strings.Contains(challengeID, "/") {
		httpError(w, r, http.StatusNotFound, "探测挑战不存在")
		return
	}
	if h.ProbeStore == nil {
		httpError(w, r, http.StatusNotFound, "探测挑战不存在")
		return
	}
	response, ok := h.ProbeStore.Response(challengeID)
	if !ok {
		httpError(w, r, http.StatusNotFound, "探测挑战不存在")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *Handler) authorizationBytes(id string) (int64, error) {
	var sent int64
	err := h.DB.QueryRow(`SELECT COALESCE(SUM(sent_bytes), 0)
		FROM pending_traffic_events WHERE authorization_id = ?`, id).Scan(&sent)
	return sent, err
}

func (h *Handler) authorizationLimit(claims downloadtoken.Claims) int64 {
	limit := claims.MaxBytes
	if claims.TrafficLimitBytes > 0 && claims.TrafficLimitBytes < limit {
		limit = claims.TrafficLimitBytes
	}
	return limit
}

func (h *Handler) localAsset(assetID string) (localAsset, error) {
	var out localAsset
	err := h.DB.QueryRow(`SELECT asset_id, relative_path, digest_sha256,
		size_bytes, COALESCE(verified_at, '') FROM local_assets
		WHERE asset_id = ? AND state = 'verified'`, assetID).
		Scan(&out.AssetID, &out.RelativePath, &out.DigestSHA256, &out.SizeBytes, &out.VerifiedAt)
	if err != nil {
		return out, err
	}
	return cleanLocalAsset(out)
}

func (h *Handler) localAssetByPath(rel, assetID string) (localAsset, error) {
	var out localAsset
	err := h.DB.QueryRow(`SELECT asset_id, relative_path, digest_sha256,
		size_bytes, COALESCE(verified_at, '')
		FROM local_assets WHERE relative_path = ? AND asset_id = ?
		AND state = 'verified'`, rel, assetID).Scan(&out.AssetID,
		&out.RelativePath, &out.DigestSHA256, &out.SizeBytes, &out.VerifiedAt)
	if err != nil {
		return localAsset{}, err
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
