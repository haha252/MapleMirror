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
	Version string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := requestid.FromContext(r.Context())
	h.Logger.Info(r.Context(), "收到下载节点健康检查请求", slog.String("request_id", id))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"状态": "正常", "角色": "下载节点", "说明": "基础服务已就绪", "版本": h.Version, "请求ID": id,
	})
}
