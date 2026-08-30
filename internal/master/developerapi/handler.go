package developerapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"mirror-server/internal/clientip"
	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

type ProjectSource interface {
	Load() (config.Projects, error)
	Current() config.Projects
}

type SyncTrigger interface {
	Trigger(context.Context, string, string) (string, error)
}

type Handler struct {
	Store        *Store
	Projects     ProjectSource
	Sync         SyncTrigger
	TrustedCIDRs []string
	Logger       *logging.Logger

	mu       sync.Mutex
	inflight map[string]struct{}
	authFail *authFailureLimiter
}

type apiResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func NewHandler(store *Store, projects ProjectSource, syncer SyncTrigger,
	trustedCIDRs []string, logger *logging.Logger) *Handler {
	return &Handler{
		Store: store, Projects: projects, Sync: syncer,
		TrustedCIDRs: trustedCIDRs, Logger: logger,
		inflight: map[string]struct{}{}, authFail: newAuthFailureLimiter(),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	projectID, ok := developerSyncPath(r.URL.Path)
	if !ok {
		h.writeError(w, r, http.StatusNotFound, "NOT_FOUND", "接口不存在")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.writeError(w, r, http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED", "请求方法不支持")
		return
	}
	if h.Store == nil || h.Sync == nil {
		h.writeError(w, r, http.StatusServiceUnavailable,
			"SYNC_UNAVAILABLE", "更新检测服务暂不可用")
		return
	}

	client := clientip.Address(r, h.TrustedCIDRs)
	now := time.Now()
	if blocked, until := h.authFail.blocked(client, now); blocked {
		w.Header().Set("Retry-After", retryAfter(now, until))
		h.writeError(w, r, http.StatusTooManyRequests,
			"AUTH_RATE_LIMITED", "认证失败次数过多，请稍后再试")
		return
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		h.authFail.failure(client, now)
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeError(w, r, http.StatusUnauthorized,
			"UNAUTHORIZED", "缺少有效的 Bearer Token")
		return
	}
	tokenProject, prefix, err := h.Store.Authenticate(r.Context(), token)
	if err != nil {
		h.authFail.failure(client, now)
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeError(w, r, http.StatusUnauthorized,
			"UNAUTHORIZED", "Developer API Token 无效")
		return
	}
	h.authFail.success(client)
	if tokenProject != projectID {
		h.writeError(w, r, http.StatusForbidden,
			"TOKEN_PROJECT_MISMATCH", "Token 无权触发该项目")
		return
	}
	if exists, enabled := h.projectState(projectID); !exists {
		h.writeError(w, r, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在")
		return
	} else if !enabled {
		h.writeError(w, r, http.StatusConflict, "PROJECT_DISABLED", "项目当前已禁用")
		return
	}

	if h.isInflight(projectID) {
		h.respondAlreadyRunning(w, r, projectID, now)
		return
	}
	running, err := h.Store.RecentRunningScan(r.Context(), projectID, now)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError,
			"STATE_CHECK_FAILED", "扫描状态检查失败")
		return
	}
	if running || !h.acquire(projectID) {
		h.respondAlreadyRunning(w, r, projectID, now)
		return
	}

	usage, err := h.Store.Consume(r.Context(), projectID, token, now)
	h.rateHeaders(w, usage)
	if err != nil {
		h.release(projectID)
		if errors.Is(err, ErrRateLimited) {
			w.Header().Set("Retry-After", retryAfter(now, usage.ResetAt))
			h.writeError(w, r, http.StatusTooManyRequests,
				"RATE_LIMIT_EXCEEDED", "今日更新检测次数已用完")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError,
			"RATE_LIMIT_FAILED", "Developer API 额度检查失败")
		return
	}

	reqID := developerRequestID(r)
	scanCtx := context.WithoutCancel(r.Context())
	go h.trigger(scanCtx, projectID, reqID, prefix, client)
	h.writeOK(w, r, http.StatusAccepted, "更新检测任务已接受", map[string]any{
		"project_id": projectID, "accepted": true, "already_running": false,
	})
}

func (h *Handler) trigger(ctx context.Context, projectID, reqID, prefix, client string) {
	defer h.release(projectID)
	scanID, err := h.Sync.Trigger(ctx, projectID, reqID)
	if h.Logger == nil {
		return
	}
	fields := []slog.Attr{
		slog.String("project_id", projectID), slog.String("request_id", reqID),
		slog.String("token_prefix", prefix), slog.String("client_ip", client),
		slog.String("scan_id", scanID),
	}
	if err != nil {
		fields = append(fields, slog.String("error", err.Error()))
		h.Logger.Warn(ctx, "Developer API 更新检测失败", fields...)
		return
	}
	h.Logger.Debug(ctx, "Developer API 更新检测完成", fields...)
}

func (h *Handler) respondAlreadyRunning(w http.ResponseWriter, r *http.Request,
	projectID string, now time.Time) {
	usage, err := h.Store.Usage(r.Context(), projectID, now)
	if err == nil {
		h.rateHeaders(w, usage)
	}
	h.writeOK(w, r, http.StatusAccepted, "项目扫描已在进行中", map[string]any{
		"project_id": projectID, "accepted": false, "already_running": true,
	})
}

func (h *Handler) projectState(projectID string) (bool, bool) {
	if h.Projects == nil {
		return false, false
	}
	projects, err := h.Projects.Load()
	if err != nil {
		projects = h.Projects.Current()
	}
	for _, project := range projects.Projects {
		if project.ID == projectID {
			return true, project.Enabled
		}
	}
	return false, false
}

func (h *Handler) acquire(projectID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.inflight[projectID]; ok {
		return false
	}
	h.inflight[projectID] = struct{}{}
	return true
}

func (h *Handler) release(projectID string) {
	h.mu.Lock()
	delete(h.inflight, projectID)
	h.mu.Unlock()
}

func (h *Handler) isInflight(projectID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.inflight[projectID]
	return ok
}
