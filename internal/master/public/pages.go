package public

import (
	"fmt"
	"html/template"
	"net/http"
)

func (s Server) statsPage(w http.ResponseWriter, r *http.Request) {
	overview, err := s.Store.StatsOverview(r.Context())
	if err != nil {
		http.Error(w, "统计数据读取失败", http.StatusInternalServerError)
		return
	}
	projects, _ := s.Store.ProjectStats(r.Context(), overview.StatDay)
	body := `<table><tr><th>统计日</th><th>下载授权次数</th><th>开始传输授权数</th><th>当日流量</th><th>累计流量</th></tr>`
	body += `<tr><td>` + esc(overview.StatDay) + `</td><td>` + num(overview.AuthorizationCount) +
		`</td><td>` + num(overview.TransferStartedCount) + `</td><td>` + bytesText(overview.DailySentBytes) +
		`</td><td>` + bytesText(overview.TotalSentBytes) + `</td></tr></table>`
	body += `<h2>项目统计</h2><table><tr><th>项目</th><th>授权次数</th><th>开始传输</th><th>实际流量</th></tr>`
	for _, p := range projects {
		body += `<tr><td>` + esc(p.ProjectID) + `</td><td>` + num(p.AuthorizationCount) +
			`</td><td>` + num(p.TransferStartedCount) + `</td><td>` + bytesText(p.SentBytes) + `</td></tr>`
	}
	body += `</table>`
	s.renderPage(w, pageData{Title: "统计数据", BodyClass: "page-stats", Body: template.HTML(body)})
}

func (s Server) aboutPage(w http.ResponseWriter, _ *http.Request) {
	body := `<p>本服务提供公开 GitHub Release 文件镜像下载。</p><p class="muted">镜像内容来自公开仓库，本服务不是 GitHub 官方服务。网页下载使用自托管 ALTCHA，公开 API 使用独立 SHA-256 前导零 PoW。</p>`
	s.renderPage(w, pageData{Title: "关于", BodyClass: "page-about", Body: template.HTML(body)})
}

func (s Server) nodesPage(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.Store.Nodes(r.Context())
	if err != nil {
		http.Error(w, "节点状态读取失败", http.StatusInternalServerError)
		return
	}
	body := `<table><tr><th>节点</th><th>状态</th><th>同步就绪</th><th>最近心跳</th><th>24h SLA</th><th>7d SLA</th><th>30d SLA</th></tr>`
	for _, n := range nodes {
		ready := "否"
		if n.RoutingReady {
			ready = "是"
		} else {
			ready += renderDetail("原因", n.RoutingReadyReason)
		}
		body += `<tr><td>` + esc(n.PublicName) + `</td><td>` + esc(n.State) +
			`</td><td>` + ready + `</td><td>` + esc(n.LastHeartbeat) +
			`</td><td>` + esc(n.SLA24H) + `</td><td>` + esc(n.SLA7D) +
			`</td><td>` + esc(n.SLA30D) + `</td></tr>`
	}
	body += `</table>`
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
