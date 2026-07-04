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

func TestHealthDoesNotClaimReadyBeforeStorage(t *testing.T) {
	logger, err := logging.New("master", config.Logging{
		ConsoleLevel: "error", FileLevel: "error", Directory: t.TempDir(), RetentionDays: 1,
	}, time.Local, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	handler := requestid.Middleware(Handler{Logger: logger, Ready: func() bool { return false }, Version: "test"}, "X-Request-ID", "X-Request-ID")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "数据库不可用") {
		t.Fatal("主节点不得在数据库不可用时报告就绪")
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("健康响应缺少请求 ID")
	}
}

func TestHealthIsReadyAfterStorage(t *testing.T) {
	logger, err := logging.New("master", config.Logging{
		ConsoleLevel: "error", FileLevel: "error", Directory: t.TempDir(), RetentionDays: 1,
	}, time.Local, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	handler := requestid.Middleware(Handler{Logger: logger, Ready: func() bool { return true }, Version: "test"}, "X-Request-ID", "X-Request-ID")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "基础服务已就绪") {
		t.Fatal("存储初始化完成后主节点应报告基础就绪")
	}
}
