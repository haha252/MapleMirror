package public

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

func (s Server) statsPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	stats, err := s.Store.StatsDashboard(r.Context())
	if err != nil {
		http.Error(w, "统计数据读取失败", http.StatusInternalServerError)
		return
	}
	nodes, _ := s.Store.Nodes(r.Context())
	s.renderPage(w, pageData{Title: "数据统计", BrowserTitle: "数据统计 - 枫源镜像", BodyClass: "page-stats",
		Body: statsBody(stats, nodes), Styles: []string{"/static/public/stats.css"},
		Scripts: []string{"/static/public/stats.js"}})
}

func (s Server) aboutPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	s.renderPage(w, pageData{Title: "关于本项目", BrowserTitle: "关于本项目 - 枫源镜像", BodyClass: "page-about",
		Subtitle: "关于枫源镜像，和为本站做出贡献的朋友们",
		Body:     aboutBody(loadSponsors()), Styles: []string{"/static/public/about.css"}})
}

func (s Server) nodesPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	nodes, err := s.Store.Nodes(r.Context())
	if err != nil {
		http.Error(w, "节点状态读取失败", http.StatusInternalServerError)
		return
	}
	body := nodesTable(nodes)
	s.renderPage(w, pageData{Title: "节点状态", BodyClass: "page-nodes", Body: template.HTML(body)})
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
	case "syncing":
		return "同步中"
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
