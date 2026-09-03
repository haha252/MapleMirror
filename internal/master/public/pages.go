package public

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

func (s Server) statsPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	s.renderPage(w, s.localizeIndexablePage(r, "/stats", pageData{Title: "数据统计", BrowserTitle: "枫源镜像节点状态与下载数据统计 - 访问、流量与 SLA",
		Description: "查看枫源镜像的访问量、下载量、传输流量、镜像节点在线状态与服务 SLA，了解最近 30 天的访问趋势、下载表现、节点健康状况和公共镜像服务运行情况，并为节点稳定性和下载服务可用性提供公开参考，便于用户了解服务质量。",
		Subtitle:    "查看节点状态、访问量、下载量、流量与近 30 日趋势。",
		BodyClass:   "page-stats",
		Body:        statsShellBody(), Styles: []string{"/static/public/stats.css"},
		Scripts: []string{"/static/public/stats-sources.js", "/static/public/stats.js"}}))
}

func (s Server) changelogPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	s.trackPageView(w, r)
	s.renderPage(w, s.localizeIndexablePage(r, "/changelog", pageData{
		Title: "更新日志", BrowserTitle: "枫源镜像更新日志 - 版本发布、功能改进与服务维护",
		Description: "查看枫源镜像的版本发布、功能更新、维护记录、服务调整与重要变更，了解镜像下载、公共 API、节点管理、安全策略和站点体验的最新改进，并按时间跟踪服务的持续变化与近期维护重点，帮助用户掌握服务演进方向。",
		Subtitle:    "按时间查看版本发布、功能更新、服务维护与重要变更。",
		BodyClass:   "page-changelog", ChangelogSearch: true, Body: changelogShellBody(s.staticURL),
		Styles: []string{
			"/static/public/download-filters.css",
			"/static/public/download-filters-mobile.css",
			"/static/public/changelog.css",
		},
		Scripts: []string{"/static/public/changelog.js"},
	}))
}

func (s Server) aboutPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	sponsors := buildSponsorPage(loadSponsors(), requestedSponsorPage(r.URL.Query().Get("sponsor_page")), sponsorPageSize)
	s.renderPage(w, s.localizeIndexablePage(r, "/about", pageData{Title: "关于本项目", BrowserTitle: "关于枫源镜像 - 公益镜像服务、开源代码与赞助支持",
		Description: "了解枫源镜像的公益目标、服务范围、开源代码、维护方式、赞助支持和问题反馈渠道，查看项目如何提供稳定、透明、可靠的 GitHub Release 下载服务，并参与共同建设，也欢迎用户参与节点贡献与社区支持。",
		BodyClass:   "page-about",
		Subtitle:    "了解枫源镜像的公益目标、开源项目、维护方式与支持方式。",
		Body:        aboutBody(sponsors, s.staticURL), Styles: []string{"/static/public/about.css"},
		Scripts: []string{"/static/public/about.js"}}))
}

func (s Server) nodesPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	nodes, err := s.Store.Nodes(r.Context())
	if err != nil {
		http.Error(w, "节点状态读取失败", http.StatusInternalServerError)
		return
	}
	body := nodesTable(nodes)
	s.renderPage(w, pageData{Title: "节点状态", Robots: noIndexRobots, BodyClass: "page-nodes", Body: template.HTML(body)})
}

func renderDetail(label, value string) string {
	if value == "" {
		return ""
	}
	return `<span class="sub">` + esc(label) + `：` + esc(value) + `</span>`
}

func esc(value string) string {
	return template.HTMLEscapeString(value)
}

func num(value int64) string {
	return template.HTMLEscapeString(fmt.Sprintf("%d", value))
}

func bytesText(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return num(value) + " B"
	}
	return template.HTMLEscapeString(fmt.Sprintf("%.2f %s", size, units[unit]))
}

func (s Server) trackPageView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		return
	}
	tracker := s.PageViews
	if tracker == nil {
		tracker = defaultPageViews
	}
	day := statDay(timeNow(), s.Store.Location)
	if tracker.shouldCount(day, visitorID(w, r)) {
		_ = s.Store.IncrementPageView(r.Context())
	}
}

func stateText(value string) string {
	switch strings.ToLower(value) {
	case "online":
		return "在线"
	case "syncing":
		return "在线"
	case "ready":
		return "在线"
	case "offline":
		return "离线"
	case "disabled":
		return "已禁用"
	case "pending":
		return "待接入"
	default:
		return value
	}
}

func stateHTML(value string) string {
	key := ""
	switch strings.ToLower(value) {
	case "online", "syncing", "ready":
		key = "stats.state.online"
	case "offline":
		key = "stats.state.offline"
	case "disabled":
		key = "stats.state.disabled"
	case "pending":
		key = "stats.state.pending"
	}
	label := esc(stateText(value))
	if key == "" {
		return label
	}
	return `<span data-i18n="` + key + `">` + label + `</span>`
}
