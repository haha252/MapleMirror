package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestLoggerUsesChineseUTF8AndIndependentLevels(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	logger, err := New("master", config.Logging{
		ConsoleLevel: "error", FileLevel: "info", Directory: dir, RetentionDays: 30,
	}, time.FixedZone("测试时区", 8*60*60), &console)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info(context.Background(), "主节点健康服务已启动")
	_ = logger.Close()
	if console.Len() != 0 {
		t.Fatal("控制台等级不应输出 info 日志")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	content, _ := os.ReadFile(files[0])
	if !strings.Contains(string(content), "主节点健康服务已启动") {
		t.Fatal("文件日志未保留 UTF-8 中文消息")
	}
	var entry map[string]any
	if err := json.Unmarshal(content, &entry); err != nil {
		t.Fatalf("文件日志应保持 JSON 格式：%v", err)
	}
	if entry["component"] != "master" {
		t.Fatalf("文件日志 component = %v", entry["component"])
	}
}

func TestLoggerRemovesExpiredDailyFile(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "node-2000-01-01.log")
	_ = os.WriteFile(old, []byte("旧日志"), 0o600)
	logger, err := New("node", config.Logging{
		ConsoleLevel: "info", FileLevel: "info", Directory: dir, RetentionDays: 2,
	}, time.Local, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	waitForMissingPath(t, old)
	_ = logger.Close()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("过期日志未清理")
	}
}

func TestConsoleLoggerUsesHumanReadableChineseFields(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	logger, err := New("master", config.Logging{
		ConsoleLevel: "info", FileLevel: "info", Directory: dir, RetentionDays: 30,
	}, time.FixedZone("测试时区", 8*60*60), &console)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info(context.Background(), "下载令牌已签发",
		slog.String("request_id", "req-1"),
		slog.String("authorization_id", "auth-1"),
		slog.String("asset_id", "asset-1"),
		slog.String("client_ip", "180.98.84.9"),
		slog.String("client_source", "180.98.84.9"),
		slog.String("node_id", "node-1"),
		slog.String("node_name", "成都-02"),
		slog.String("project_id", "FoldCraftLauncher"),
		slog.String("system", ""),
		slog.String("architecture", "arm64-v8a"),
		slog.String("client_prefix", "180.98.84.9/32"),
		slog.Any("request_remaining_tokens", map[string]int64{"ipv4_24": 594, "ipv4_32": 114}),
		slog.Any("request_remaining_microunits", map[string]int64{"ipv4_24": 594000000}),
		slog.Any("traffic_remaining_bytes", map[string]int64{"ipv4_24": 21474836480, "ipv4_32": 3221225472}))
	_ = logger.Close()

	line := console.String()
	if strings.HasPrefix(line, "{") || strings.Contains(line, `"msg"`) {
		t.Fatalf("控制台日志不应再使用 JSON：%s", line)
	}
	for _, want := range []string{
		"信息 master 下载令牌已签发",
		"，客户端=180.98.84.9",
		"，客户端来源=180.98.84.9",
		"，节点=成都-02",
		"，项目=FoldCraftLauncher",
		"，系统=",
		"，架构=arm64-v8a",
		"，剩余令牌={ipv4_24=594, ipv4_32=114}",
		"，剩余流量={ipv4_24=20GB, ipv4_32=3GB}",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("控制台日志缺少 %q：%s", want, line)
		}
	}
	for _, hidden := range []string{"请求=", "授权=", "资产=", "客户端前缀=", "剩余请求微单位="} {
		if strings.Contains(line, hidden) {
			t.Fatalf("下载令牌控制台日志不应包含 %q：%s", hidden, line)
		}
	}
}
