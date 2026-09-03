package public

import (
	"strings"
	"testing"
)

func assertDownloadCardLayout(t *testing.T, body string) {
	t.Helper()
	ordered := []string{
		`class="project-name"`,
		`class="muted version-badge"`,
		`class="muted project-repository"`,
		`class="selection-mode"`,
		`class="muted project-updated"`,
		`class="project-card__body"`,
	}
	previous := -1
	for _, marker := range ordered {
		position := strings.Index(body, marker)
		if position <= previous {
			t.Fatalf("卡片头部项目名、版本、仓库、选择方式和最近更新顺序错误：%s", body)
		}
		previous = position
	}
	if strings.Contains(body, `class="project-availability"`) ||
		!strings.Contains(body, `class="download-button" data-i18n="download.unavailable" disabled>暂不可下载</button>`) {
		t.Fatalf("卡片应仅通过下载按钮表达当前文件是否可下载：%s", body)
	}
}
