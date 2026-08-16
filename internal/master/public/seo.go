package public

import (
	"strings"
)

const noIndexRobots = "noindex,nofollow"

func (s Server) canonicalURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(s.PublicBaseURL), "/")
	if base == "" || path == "" || !strings.HasPrefix(path, "/") {
		return ""
	}
	return base + path
}

func projectMetaDescription(name, description string) string {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	base := name + " 的 GitHub Release 版本与文件下载页面，查看最新版本、历史版本、文件列表、发布日期与镜像可用状态。"
	if description != "" {
		base = name + "：" + description + " 在枫源镜像查看 GitHub Release 版本、文件列表、发布日期与镜像下载状态。"
	}
	return truncateRunes(base, 160)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
