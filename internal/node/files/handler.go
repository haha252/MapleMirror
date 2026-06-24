package files

import (
	"database/sql"
	"encoding/json"
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
	if r.Method != http.MethodGet {
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
	asset, err := h.requestedAsset(requested, claims.AssetID)
	if err != nil {
		httpError(w, r, http.StatusNotFound, "本地资产不可用")
		return
	}
	if claims.AssetID != asset.AssetID {
		httpError(w, r, http.StatusUnauthorized, "下载令牌无效")
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
		sent: sent, timing: timing}
	http.ServeContent(counter, r, filepath.Base(asset.RelativePath), info.ModTime(), file)
	if counter.bytes > 0 {
		if err := h.recordTraffic(claims, asset.AssetID, requestid.FromContext(r.Context()), counter.bytes); err != nil && h.Logger != nil {
			h.Logger.Warn(r.Context(), "下载流量事件记录失败",
				slog.String("request_id", requestid.FromContext(r.Context())),
				slog.String("authorization_id", claims.AuthorizationID),
				slog.String("asset_id", asset.AssetID),
				slog.String("error", err.Error()))
		}
	}
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
