package logging

import (
	"bytes"
	"context"
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
	_ = logger.Close()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("过期日志未清理")
	}
}
