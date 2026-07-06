package accountingarchive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintainCompressesClosedAndRemovesExpiredArchives(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "control_session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-07-06.jsonl", "2026-07-05.jsonl", "2025-07-05.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if err := Maintain(root, 365, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-07-06.jsonl")); err != nil {
		t.Fatal("当天归档不应压缩", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-07-05.jsonl.gz")); err != nil {
		t.Fatal("昨日归档应压缩", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-07-05.jsonl")); !os.IsNotExist(err) {
		t.Fatal("压缩后应删除源文件")
	}
	if _, err := os.Stat(filepath.Join(dir, "2025-07-05.jsonl")); !os.IsNotExist(err) {
		t.Fatal("过期归档应删除")
	}
}
