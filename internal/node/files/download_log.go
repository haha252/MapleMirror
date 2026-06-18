package files

import (
	"log/slog"
	"net/http"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/requestid"
)

func (h *Handler) logDownloadRequestOnce(r *http.Request, claims downloadtoken.Claims) {
	if h.Logger == nil {
		return
	}
	h.mu.Lock()
	if h.downloadLogs == nil {
		h.downloadLogs = make(map[string]struct{})
	}
	if _, ok := h.downloadLogs[claims.AuthorizationID]; ok {
		h.mu.Unlock()
		return
	}
	h.downloadLogs[claims.AuthorizationID] = struct{}{}
	h.mu.Unlock()
	h.Logger.Info(r.Context(), "下载节点收到下载请求",
		slog.String("request_id", requestid.FromContext(r.Context())),
		slog.String("authorization_id", claims.AuthorizationID),
		slog.String("asset_id", claims.AssetID),
		slog.String("client_ip", h.clientIP(r)),
		slog.String("project_id", claims.ProjectID),
		slog.String("system", claims.System),
		slog.String("architecture", claims.Architecture))
}
