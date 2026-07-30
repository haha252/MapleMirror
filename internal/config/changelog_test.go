package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadChangelogSortsFilesAndParsesBeijingTime(t *testing.T) {
	root := t.TempDir()
	writeChangelogTestFile(t, root, "2026-07/z.yaml", `level: info
title: 较早
time: "2026-07-29 08:10"
description: 第一条
`)
	writeChangelogTestFile(t, root, "2026-07/a.yaml", `level: critical
title: 较新
time: "2026-07-30 12:20"
description: 第二条
`)
	events, err := LoadChangelog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Title != "较新" || events[1].Title != "较早" {
		t.Fatalf("更新日志顺序错误：%+v", events)
	}
	if got := events[0].OccurredAt.Format("-07:00"); got != "+08:00" {
		t.Fatalf("更新时间应解释为北京时间：%s", got)
	}
}

func TestLoadChangelogUsesStablePathOrderForSameMinute(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b.yaml", "a.yaml"} {
		writeChangelogTestFile(t, root, "2026-07/"+name, `level: notice
title: 同一分钟
time: "2026-07-30 12:20"
description: ""
`)
	}
	events, err := LoadChangelog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].SourcePath != "2026-07/a.yaml" {
		t.Fatalf("同一分钟未按路径稳定排序：%+v", events)
	}
}

func TestLoadChangelogAllowsMissingDirectory(t *testing.T) {
	events, err := LoadChangelog(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(events) != 0 {
		t.Fatalf("缺失目录应返回空记录：events=%+v err=%v", events, err)
	}
}

func TestPublishedChangelogExampleLoads(t *testing.T) {
	root := filepath.Join("..", "..", "configs", "changelog.example")
	events, err := LoadChangelog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Title != "上线更新日志页面" {
		t.Fatalf("发布的更新日志示例无效：%+v", events)
	}
}

func TestLoadChangelogRejectsInvalidFiles(t *testing.T) {
	cases := map[string]string{
		"未知字段": `level: info
title: 标题
time: "2026-07-30 12:20"
unknown: true
`,
		"非法等级": `level: debug
title: 标题
time: "2026-07-30 12:20"
`,
		"非法时间": `level: info
title: 标题
time: "2026/07/30"
`,
		"月份不符": `level: info
title: 标题
time: "2026-08-01 00:00"
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeChangelogTestFile(t, root, "2026-07/event.yaml", body)
			if _, err := LoadChangelog(root); err == nil {
				t.Fatal("应拒绝无效更新日志")
			}
		})
	}
}

func writeChangelogTestFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
