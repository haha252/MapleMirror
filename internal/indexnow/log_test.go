package indexnow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

func TestSubmissionSuccessLogContainsStatisticsWithoutResponseBody(t *testing.T) {
	requests := make(chan requestPayload, 4)
	transport := &recordingTransport{
		statuses:       []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusOK},
		responseBodies: []string{"secret-429", "secret-429", "secret-success"}, payloads: requests,
	}
	logger, manager, directory := newLoggedManager(t, transport)
	manager.NotifyPaths(context.Background(), []string{"/about", "/about", "/stats"})
	for i := 0; i < 3; i++ {
		waitPayload(t, requests)
	}
	manager.Close()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	entries, raw := readIndexNowLog(t, directory)
	entry := findLogEntry(t, entries, "IndexNow URL 提交成功")
	if got := entry["component"]; got != "indexnow" {
		t.Fatalf("component=%v, want indexnow", got)
	}
	if entry["url_count"] != float64(2) || entry["attempts"] != float64(3) || entry["status"] != float64(http.StatusOK) {
		t.Fatalf("submission statistics=%v", entry)
	}
	if entry["retry_scheduled"] != false {
		t.Fatalf("successful submission retry_scheduled=%v, want false", entry["retry_scheduled"])
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("success log missing duration_ms: %v", entry)
	}
	if strings.Contains(raw, "secret-") {
		t.Fatalf("response body leaked into IndexNow log: %s", raw)
	}
}

func TestPermanentSubmissionFailureLogDoesNotRetryForever(t *testing.T) {
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusBadRequest}, responseBodies: []string{"secret-400"}, payloads: requests}
	logger, manager, directory := newLoggedManager(t, transport)
	manager.NotifyPaths(context.Background(), []string{"/about"})
	waitPayload(t, requests)
	manager.Close()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	entries, raw := readIndexNowLog(t, directory)
	entry := findLogEntry(t, entries, "IndexNow URL 批次最终失败")
	if entry["url_count"] != float64(1) || entry["attempts"] != float64(1) || entry["status"] != float64(http.StatusBadRequest) {
		t.Fatalf("permanent failure statistics=%v", entry)
	}
	if entry["retry_scheduled"] != false {
		t.Fatalf("permanent failure retry_scheduled=%v, want false", entry["retry_scheduled"])
	}
	if entry["failure_kind"] != "permanent_error" {
		t.Fatalf("permanent failure kind=%v", entry["failure_kind"])
	}
	if transport.attempts != 1 || strings.Contains(raw, "secret-400") {
		t.Fatalf("permanent failure retried or leaked response body: attempts=%d log=%s", transport.attempts, raw)
	}
}

func TestTransientSubmissionFailureLogRecordsScheduledRetry(t *testing.T) {
	requests := make(chan requestPayload, 4)
	transport := &recordingTransport{
		statuses:       []int{http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError},
		responseBodies: []string{"secret-500", "secret-500", "secret-500", "secret-500"}, payloads: requests,
	}
	logger, manager, directory := newLoggedManager(t, transport)
	manager.NotifyPaths(context.Background(), []string{"/stats"})
	for i := 0; i < 4; i++ {
		waitPayload(t, requests)
	}
	manager.Close()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	entries, raw := readIndexNowLog(t, directory)
	entry := findLogEntry(t, entries, "IndexNow URL 批次最终失败")
	if entry["attempts"] != float64(maxAttempts) || entry["status"] != float64(http.StatusInternalServerError) || entry["retry_scheduled"] != true {
		t.Fatalf("transient failure statistics=%v", entry)
	}
	if entry["failure_kind"] != "retry_exhausted" {
		t.Fatalf("transient failure kind=%v", entry["failure_kind"])
	}
	if strings.Contains(raw, "secret-500") {
		t.Fatalf("response body leaked into transient failure log: %s", raw)
	}
}

func TestNetworkSubmissionFailureLogRecordsRetryExhaustion(t *testing.T) {
	requests := make(chan requestPayload, 4)
	transport := &recordingTransport{roundTripErr: errors.New("network unavailable"), payloads: requests}
	logger, manager, directory := newLoggedManager(t, transport)
	manager.NotifyPaths(context.Background(), []string{"/stats"})
	for i := 0; i < maxAttempts; i++ {
		waitPayload(t, requests)
	}
	manager.Close()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	entries, _ := readIndexNowLog(t, directory)
	entry := findLogEntry(t, entries, "IndexNow URL 批次最终失败")
	if entry["attempts"] != float64(maxAttempts) || entry["status"] != float64(0) || entry["retry_scheduled"] != true {
		t.Fatalf("network failure statistics=%v", entry)
	}
	if entry["failure_kind"] != "retry_exhausted" {
		t.Fatalf("network failure kind=%v", entry["failure_kind"])
	}
}

func TestBootstrapLogContainsCountWithoutURLList(t *testing.T) {
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	logger, manager, directory := newLoggedManager(t, transport)
	manager.Bootstrap([]string{"p1", "p2"}, "revision-1")
	waitPayload(t, requests)
	manager.Close()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	entries, raw := readIndexNowLog(t, directory)
	entry := findLogEntry(t, entries, "IndexNow bootstrap 已排队")
	if entry["url_count"] != float64(7) || strings.Contains(raw, "https://fyhub.cn/") {
		t.Fatalf("bootstrap log should contain only count: entry=%v raw=%s", entry, raw)
	}
}

func newLoggedManager(t *testing.T, transport *recordingTransport) (*logging.Logger, *Manager, string) {
	t.Helper()
	directory := t.TempDir()
	logger, err := logging.New("indexnow", config.Logging{ConsoleLevel: "error", FileLevel: "info",
		Directory: directory, RetentionDays: 30, MaxFileSizeMB: 1}, time.UTC, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: filepath.Join(directory, "state"),
		HTTPClient: &http.Client{Transport: transport}, Logger: logger})
	if err != nil {
		_ = logger.Close()
		t.Fatal(err)
	}
	return logger, manager, directory
}

func readIndexNowLog(t *testing.T, directory string) ([]map[string]any, string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(directory, "indexnow-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("IndexNow 日志文件=%v err=%v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("IndexNow log line is not JSON: %v; line=%s", err, line)
		}
		entries = append(entries, entry)
	}
	return entries, string(data)
}

func findLogEntry(t *testing.T, entries []map[string]any, message string) map[string]any {
	t.Helper()
	for _, entry := range entries {
		if entry["msg"] == message {
			return entry
		}
	}
	t.Fatalf("log message %q not found: %v", message, entries)
	return nil
}
