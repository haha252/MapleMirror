package files

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
)

func TestHandlerLogsDownloadRequestOncePerAuthorization(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	var console bytes.Buffer
	logger, err := logging.New("node", config.Logging{
		ConsoleLevel: "info", FileLevel: "error", Directory: t.TempDir(), RetentionDays: 30,
	}, time.UTC, &console)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1",
		Signer: signer, Logger: logger}
	serveDownload := func(authID string) {
		t.Helper()
		claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
			AuthorizationID: authID, AssetID: "asset-1", NodeID: "node-1",
			ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
			MaxBytes: 100, RangeConcurrencyLimit: 2, RequestID: "req-" + authID,
			ProjectID: "p1", System: "linux", Architecture: "amd64"}
		token, err := signer.Sign(claims)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("下载响应不符合预期：code=%d body=%q", rec.Code, rec.Body.String())
		}
	}
	serveDownload("auth-1")
	serveDownload("auth-1")
	serveDownload("auth-2")
	if got := strings.Count(console.String(), "下载节点收到下载请求"); got != 2 {
		t.Fatalf("下载请求日志应按授权只提示一次，got=%d log=%s", got, console.String())
	}
}
