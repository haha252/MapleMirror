package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

func TestIndexNowLogDirectoryIsSiblingOfMasterDirectory(t *testing.T) {
	for _, test := range []struct {
		master, want string
	}{
		{"logs/master", "logs/indexnow"},
		{"/var/log/mirror/master", "/var/log/mirror/indexnow"},
	} {
		if got := indexNowLogDirectory(test.master); got != test.want {
			t.Errorf("indexNowLogDirectory(%q)=%q, want %q", test.master, got, test.want)
		}
	}
}

func TestStartIndexNowWritesDisabledEventToSeparateJSONLog(t *testing.T) {
	root := t.TempDir()
	masterConfig := config.Logging{
		ConsoleLevel: "error", FileLevel: "info", Directory: filepath.Join(root, "master"),
		RetentionDays: 30, MaxFileSizeMB: 1,
	}
	masterLogger, err := logging.New("master", masterConfig, time.UTC, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	cfg := config.Master{Logging: masterConfig, Server: config.MasterServer{PublicBaseURL: "https://mirror.example.com"},
		IndexNow: config.IndexNow{Enabled: &enabled}}
	manager, indexLogger := startIndexNow(cfg, time.UTC, masterLogger)
	if manager != nil || indexLogger == nil {
		t.Fatalf("disabled IndexNow should keep only its logger: manager=%v logger=%v", manager, indexLogger)
	}
	if err := indexLogger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := masterLogger.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(root, "indexnow", "indexnow-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("IndexNow 日志文件=%v err=%v", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "IndexNow 通知已禁用") {
		t.Fatalf("disabled event missing from log: %s", content)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("IndexNow log line is not JSON: %v; line=%s", err, line)
		}
		if entry["component"] != "indexnow" {
			t.Fatalf("component=%v, want indexnow", entry["component"])
		}
	}
}

func TestStartIndexNowLoggerFailureFallsBackToMasterLogger(t *testing.T) {
	root := t.TempDir()
	blockedDirectory := filepath.Join(root, "indexnow")
	if err := os.WriteFile(blockedDirectory, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	masterConfig := config.Logging{ConsoleLevel: "error", FileLevel: "info", Directory: filepath.Join(root, "master"), RetentionDays: 30, MaxFileSizeMB: 1}
	masterLogger, err := logging.New("master", masterConfig, time.UTC, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	cfg := config.Master{Logging: config.Logging{ConsoleLevel: "error", FileLevel: "info", Directory: filepath.Join(root, "master"), RetentionDays: 30, MaxFileSizeMB: 1},
		IndexNow: config.IndexNow{Enabled: &enabled}}
	manager, indexLogger := startIndexNow(cfg, time.UTC, masterLogger)
	if manager != nil || indexLogger != nil {
		t.Fatalf("logger failure should disable IndexNow without returning resources: manager=%v logger=%v", manager, indexLogger)
	}
	if err := masterLogger.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "master", "master-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("主节点日志文件=%v err=%v", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "IndexNow 独立日志初始化失败") {
		t.Fatalf("fallback event missing from master log: %s", content)
	}
}
