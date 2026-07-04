package logging

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDailyWriterCompressesPreviousLogsOnStartup(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "master-2000-01-02.log")
	content := []byte("第一行日志\n第二行日志\n")
	if err := os.WriteFile(oldPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, "master", 30_000, time.Local, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	waitForPath(t, oldPath+".gz")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("原始日志应被删除，stat err = %v", err)
	}
	got := readGzipFile(t, oldPath+".gz")
	if string(got) != string(content) {
		t.Fatalf("压缩日志内容 = %q，want %q", got, content)
	}
}

func TestDailyWriterKeepsCurrentLogUncompressed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().In(time.Local)
	currentPath := filepath.Join(dir, "node-"+now.Format(logDateLayout)+".log")
	if err := os.WriteFile(currentPath, []byte("当前日志"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, "node", 30, time.Local, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("当天日志不应被压缩或删除：%v", err)
	}
	if _, err := os.Stat(currentPath + ".gz"); !os.IsNotExist(err) {
		t.Fatalf("当天日志不应生成 gzip，stat err = %v", err)
	}
}

func TestDailyWriterSkipsExistingCompressedLog(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "master-2000-01-03.log")
	gzipPath := oldPath + ".gz"
	if err := os.WriteFile(oldPath, []byte("未压缩日志"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gzipPath, []byte("已有压缩文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, "master", 30_000, time.Local, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("已有 gzip 时原始日志应保留：%v", err)
	}
	got, err := os.ReadFile(gzipPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "已有压缩文件" {
		t.Fatalf("已有 gzip 不应被覆盖：%q", got)
	}
}

func TestDailyWriterRemovesExpiredCompressedLog(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "node-2000-01-01.log.gz")
	if err := os.WriteFile(oldPath, []byte("旧压缩日志"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, "node", 2, time.Local, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	waitForMissingPath(t, oldPath)
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("过期 gzip 日志应被清理，stat err = %v", err)
	}
}

func TestDailyWriterRemovesExpiredSegmentedLog(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "node-2000-01-01.001.log.gz")
	if err := os.WriteFile(oldPath, []byte("旧分片日志"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, "node", 2, time.Local, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	waitForMissingPath(t, oldPath)
}

func TestDailyWriterMaintenanceClosesAndCompressesStaleOpenLog(t *testing.T) {
	dir := t.TempDir()
	location := time.FixedZone("测试时区", 8*60*60)
	oldDate := "2026-07-02"
	oldPath := filepath.Join(dir, "master-"+oldDate+".log")
	file, err := os.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("跨日之前的日志\n"); err != nil {
		t.Fatal(err)
	}
	writer := &dailyWriter{
		directory: dir,
		component: "master",
		retention: 30,
		location:  location,
		date:      oldDate,
		file:      file,
	}
	now := time.Date(2026, 7, 3, 0, 1, 0, 0, location)
	if err := writer.maintain(now); err != nil {
		t.Fatal(err)
	}
	if writer.file != nil || writer.date != "" {
		t.Fatalf("跨日维护后应关闭当前旧日志，file=%v date=%q", writer.file, writer.date)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("旧日志应被压缩后删除，stat err = %v", err)
	}
	got := readGzipFile(t, oldPath+".gz")
	if string(got) != "跨日之前的日志\n" {
		t.Fatalf("压缩日志内容 = %q", got)
	}
}

func TestDailyWriterCompressionDoesNotBlockWrites(t *testing.T) {
	dir := t.TempDir()
	location := time.FixedZone("测试时区", 8*60*60)
	oldPath := filepath.Join(dir, "master-2026-07-02.log")
	if err := os.WriteFile(oldPath, []byte("旧日志"), 0o600); err != nil {
		t.Fatal(err)
	}
	compressionStarted := make(chan struct{})
	releaseCompression := make(chan struct{})
	var once sync.Once
	writer := &dailyWriter{
		directory: dir,
		component: "master",
		retention: 30,
		location:  location,
		compress: func(string) error {
			once.Do(func() { close(compressionStarted) })
			<-releaseCompression
			return nil
		},
	}
	now := time.Date(2026, 7, 3, 0, 1, 0, 0, location)
	maintenanceDone := make(chan error, 1)
	go func() { maintenanceDone <- writer.maintain(now) }()
	<-compressionStarted

	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("新日志\n"))
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("压缩历史日志时不应阻塞当日日志写入")
	}
	close(releaseCompression)
	if err := <-maintenanceDone; err != nil {
		t.Fatal(err)
	}
	if writer.file != nil {
		_ = writer.file.Close()
	}
}

func TestDailyWriterReportsFailureAndRetries(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "master-2000-01-02.log")
	if err := os.WriteFile(oldPath, []byte("需要重试的日志"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errorOutput bytes.Buffer
	succeeded := make(chan struct{})
	attempts := 0
	writer := &dailyWriter{
		directory:       dir,
		component:       "master",
		retention:       30_000,
		location:        time.Local,
		maintenanceWake: make(chan struct{}, 1),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
		errorOutput:     &errorOutput,
		retryDelay:      10 * time.Millisecond,
		compress: func(path string) error {
			attempts++
			if attempts == 1 {
				return errors.New("测试压缩故障")
			}
			err := compressLogFile(path)
			if err == nil {
				close(succeeded)
			}
			return err
		},
	}
	go writer.runMaintenanceLoop()
	writer.requestMaintenance()
	select {
	case <-succeeded:
	case <-time.After(2 * time.Second):
		t.Fatal("日志压缩失败后未按期重试")
	}
	close(writer.stop)
	<-writer.done
	if attempts < 2 {
		t.Fatalf("压缩尝试次数 = %d，want >= 2", attempts)
	}
	message := errorOutput.String()
	if !strings.Contains(message, "日志后台维护失败") ||
		!strings.Contains(message, "测试压缩故障") ||
		!strings.Contains(message, "10ms 后重试") {
		t.Fatalf("维护错误输出不完整：%s", message)
	}
}

func TestDailyWriterRotatesAndCompressesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	writer, err := newDailyWriter(dir, "master", 30, time.Local, 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := writer.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("ABCD")); err != nil {
		t.Fatal(err)
	}
	date := time.Now().In(time.Local).Format(logDateLayout)
	segmentPath := filepath.Join(dir, "master-"+date+".001.log.gz")
	waitForPath(t, segmentPath)
	if got := string(readGzipFile(t, segmentPath)); got != "12345678" {
		t.Fatalf("封存分片内容 = %q，want %q", got, "12345678")
	}
	activePath := filepath.Join(dir, "master-"+date+".log")
	active, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "ABCD" {
		t.Fatalf("活动日志内容 = %q，want %q", active, "ABCD")
	}
}

func TestDailyWriterContinuesSegmentSequenceAfterRestart(t *testing.T) {
	dir := t.TempDir()
	date := time.Now().In(time.Local).Format(logDateLayout)
	activePath := filepath.Join(dir, "node-"+date+".log")
	if err := os.WriteFile(activePath, []byte("1234567890"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node-"+date+".001.log.gz"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := newDailyWriter(dir, "node", 30, time.Local, 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := writer.Write([]byte("X")); err != nil {
		t.Fatal(err)
	}
	secondSegment := filepath.Join(dir, "node-"+date+".002.log.gz")
	waitForPath(t, secondSegment)
	if got := string(readGzipFile(t, secondSegment)); got != "1234567890" {
		t.Fatalf("重启后封存分片内容 = %q", got)
	}
}

func TestDailyWriterCloseIsIdempotent(t *testing.T) {
	writer, err := newDailyWriter(t.TempDir(), "node", 30, time.Local, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNextDailyMaintenanceDelayUsesLocalDateBoundary(t *testing.T) {
	location := time.FixedZone("测试时区", 8*60*60)
	now := time.Date(2026, 7, 2, 23, 59, 30, 0, location)
	if got := nextDailyMaintenanceDelay(now); got != 90*time.Second {
		t.Fatalf("maintenance delay = %v，want %v", got, 90*time.Second)
	}
}

func TestCompressLogFileUsesBestSpeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master-2000-01-02.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("debug log line\n"), 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := compressLogFile(path); err != nil {
		t.Fatal(err)
	}
	compressed, err := os.ReadFile(path + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) < 10 || compressed[8] != 4 {
		t.Fatalf("gzip XFL = %d，want 4（BestSpeed）", compressed[8])
	}
}

func readGzipFile(t *testing.T, path string) []byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := os.ReadFile(path)
	if err == nil && len(content) == 0 {
		t.Fatal("gzip 文件为空")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待文件 %s 超时", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForMissingPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待文件 %s 删除超时", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
