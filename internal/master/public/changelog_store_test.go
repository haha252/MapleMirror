package public

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChangelogStoreKeepsValidSnapshotAfterInvalidReload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026-07", "event.yaml")
	writeChangelogStoreFile(t, path, `level: info
title: 初始记录
time: "2026-07-30 12:20"
description: 初始描述
`)
	store := newChangelogStore(root, nil)
	defer store.close()
	first := store.current()
	if len(first.Entries) != 1 || first.Entries[0].Title != "初始记录" {
		t.Fatalf("初始快照错误：%+v", first)
	}
	writeChangelogStoreFile(t, path, "level: invalid\n")
	if store.reloadIfChanged() {
		t.Fatal("无效更新不应替换快照")
	}
	kept := store.current()
	if kept.Generation != first.Generation || kept.Entries[0].Title != "初始记录" {
		t.Fatalf("无效更新后未保留旧快照：%+v", kept)
	}
	writeChangelogStoreFile(t, path, `level: warn
title: 修复后的记录
time: "2026-07-30 12:21"
description: 已恢复
`)
	if !store.reloadIfChanged() {
		t.Fatal("修复配置后应完成热重载")
	}
	current := store.current()
	if current.Generation == first.Generation || current.Entries[0].Title != "修复后的记录" {
		t.Fatalf("修复后的快照错误：%+v", current)
	}
}

func TestChangelogStoreDetectsDirectoryCreatedLater(t *testing.T) {
	root := filepath.Join(t.TempDir(), "changelog")
	store := newChangelogStore(root, nil)
	defer store.close()
	if len(store.current().Entries) != 0 {
		t.Fatal("缺失目录应使用空快照")
	}
	writeChangelogStoreFile(t, filepath.Join(root, "2026-07", "event.yaml"), `level: notice
title: 新记录
time: "2026-07-30 12:20"
description: 新描述
`)
	if !store.reloadIfChanged() || len(store.current().Entries) != 1 {
		t.Fatal("目录创建后应自动载入记录")
	}
}

func writeChangelogStoreFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
