package health

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"mirror-server/internal/logging"
	"mirror-server/internal/requestid"
)

type Handler struct {
	Logger  *logging.Logger
	Ready   func() bool
	Version string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestid.FromContext(r.Context())
	status, explanation, code := "正常", "基础服务已就绪", http.StatusOK
	if !h.Ready() {
		status, explanation, code = "未就绪", "数据库不可用", http.StatusServiceUnavailable
	}
	h.Logger.Info(r.Context(), "收到主节点健康检查请求", slog.String("request_id", id))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"状态": status, "角色": "主节点", "说明": explanation, "版本": h.Version, "请求ID": id,
	})
}
