package public

import (
	"strings"
	"testing"
)

func TestRenderChangelogDescriptionSupportsRestrictedMarkup(t *testing.T) {
	got := renderChangelogDescription(
		"新增 **粗体和下划线**、*斜体*、~~删除线~~ 和 [文档](https://example.com/docs)。\n下一行",
	)
	for _, want := range []string{
		"<strong>粗体和下划线</strong>",
		"<em>斜体</em>",
		"<del>删除线</del>",
		`<a href="https://example.com/docs" target="_blank" rel="noopener noreferrer">文档</a>`,
		"<br>下一行",
	} {
		if !strings.Contains(got.HTML, want) {
			t.Fatalf("缺少受限格式 %q：%s", want, got.HTML)
		}
	}
	if strings.Contains(got.Text, "**") || strings.Contains(got.Text, "https://") ||
		!strings.Contains(got.Text, "粗体和下划线") || !strings.Contains(got.Text, "文档") {
		t.Fatalf("搜索文本未移除格式和链接地址：%q", got.Text)
	}
}

func TestRenderChangelogDescriptionEscapesUnsafeContent(t *testing.T) {
	got := renderChangelogDescription(
		`<script>alert(1)</script> [危险](javascript:alert(1)) [凭据](https://u:p@example.com)`,
	)
	if strings.Contains(got.HTML, "<script>") || strings.Contains(got.HTML, `href="javascript:`) ||
		strings.Contains(got.HTML, `href="https://u:p@`) {
		t.Fatalf("危险内容不应生成可执行 HTML：%s", got.HTML)
	}
	for _, want := range []string{
		"&lt;script&gt;", "[危险](javascript:alert(1))", "[凭据](https://u:p@example.com)",
	} {
		if !strings.Contains(got.HTML, want) {
			t.Fatalf("危险或不支持的标记应作为文本保留 %q：%s", want, got.HTML)
		}
	}
}

func TestRenderChangelogDescriptionHandlesNestedAndMalformedMarkup(t *testing.T) {
	got := renderChangelogDescription(`**外层 *内层* 文本**，未闭合 **标记`)
	if !strings.Contains(got.HTML, `<strong>外层 <em>内层</em> 文本</strong>`) ||
		!strings.Contains(got.HTML, `未闭合 **标记`) {
		t.Fatalf("嵌套或畸形标记处理错误：%s", got.HTML)
	}
}
