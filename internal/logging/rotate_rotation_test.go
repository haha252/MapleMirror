package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
