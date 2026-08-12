package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNoticesExample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notices.yaml")
	if err := os.WriteFile(path, NoticesExample, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadNotices(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Notices) != 1 || c.Notices[0].Level != "warn" ||
		!strings.Contains(c.Notices[0].Message, "示例公告") {
		t.Fatalf("公告示例配置缺失：%+v", c.Notices)
	}
}

func TestLoadNoticesRejectsInvalidNotices(t *testing.T) {
	cases := []string{
		`level: debug
    message: invalid`,
		`level: info
    message: ""`,
	}
	for _, replacement := range cases {
		t.Run(replacement, func(t *testing.T) {
			text := strings.Replace(string(NoticesExample),
				`level: "warn"
    # 公告正文。
    message: "这是一个示例公告，请根据实际情况修改。"`,
				replacement, 1)
			path := filepath.Join(t.TempDir(), "notices.yaml")
			_ = os.WriteFile(path, []byte(text), 0o600)
			if _, err := LoadNotices(path, nil); err == nil {
				t.Fatalf("应拒绝非法公告配置：%s", replacement)
			}
		})
	}
}
