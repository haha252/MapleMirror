package logging

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
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
	writer, err := newDailyWriter(dir, "master", 30_000, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
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
	writer, err := newDailyWriter(dir, "node", 30, time.Local)
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
	writer, err := newDailyWriter(dir, "master", 30_000, time.Local)
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
	writer, err := newDailyWriter(dir, "node", 2, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("过期 gzip 日志应被清理，stat err = %v", err)
	}
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

func TestDailyWriterCloseIsIdempotent(t *testing.T) {
	writer, err := newDailyWriter(t.TempDir(), "node", 30, time.Local)
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
