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
	s.renderPage(w, pageData{Title: "数据洞察", BodyClass: "page-stats",
		Body: statsBody(stats, nodes), Styles: []string{"/static/public/stats.css"},
		Scripts: []string{"/static/public/stats.js"}})
}

func (s Server) aboutPage(w http.ResponseWriter, r *http.Request) {
	s.trackPageView(w, r)
	body := `<p>本服务提供公开 GitHub Release 文件镜像下载。</p><p class="muted">镜像内容来自公开仓库，本服务不是 GitHub 官方服务。网页下载使用自托管 ALTCHA，公开 API 使用独立 SHA-256 前导零 PoW。</p>`
	s.renderPage(w, pageData{Title: "关于", BodyClass: "page-about", Body: template.HTML(body)})
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
	if value < 1024*1024 {
		return num(value) + " B"
	}
	return template.HTMLEscapeString(fmt.Sprintf("%.2f MiB", float64(value)/(1024*1024)))
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
