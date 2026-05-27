package health

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/requestid"
)

func TestHealthReportsOnlyBasicReadiness(t *testing.T) {
	logger, err := logging.New("node", config.Logging{
		ConsoleLevel: "error", FileLevel: "error", Directory: t.TempDir(), RetentionDays: 1,
	}, time.Local, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	handler := requestid.Middleware(Handler{Logger: logger, Version: "test"}, "X-Request-ID", "X-Request-ID")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "基础服务已就绪") || strings.Contains(body, "可路由") {
		t.Fatal("下载节点健康响应不得声明同步或可路由状态")
	}
}
