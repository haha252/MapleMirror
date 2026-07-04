package logging

import (
	"compress/gzip"
	"io"
	"os"
	"testing"
	"time"
)

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
