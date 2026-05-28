package public

import (
	"html/template"
	"net/http"
)

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;margin:0;background:#f7f8fa;color:#1f2933}
header{background:#fff;border-bottom:1px solid #dde3ea;padding:16px 24px}
nav a{margin-right:16px;color:#0b5cad;text-decoration:none}
main{max-width:960px;margin:0 auto;padding:24px}
table{width:100%;border-collapse:collapse;background:#fff}
th,td{border-bottom:1px solid #e5e9ef;padding:10px;text-align:left}
.muted{color:#667085}.ok{color:#067647}.warn{color:#b54708}
</style>
</head>
<body>
<header><nav>
<a href="/">下载</a><a href="/stats">统计数据</a><a href="/about">关于</a><a href="/nodes">节点状态</a>
</nav></header>
<main><h1>{{.Title}}</h1>{{.Body}}</main>
</body></html>`))

type pageData struct {
	Title string
	Body  template.HTML
}

func (s Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		http.Error(w, "项目列表读取失败", http.StatusInternalServerError)
		return
	}
	body := `<p class="muted">选择可用资产后将通过 ALTCHA 验证领取下载授权。</p><table><tr><th>项目</th><th>仓库</th><th>状态</th></tr>`
	for _, p := range projects {
		state := `<span class="warn">暂不可下载</span>`
		if p.Available {
			state = `<span class="ok">可下载</span>`
		}
		body += `<tr><td>` + esc(p.DisplayName) + `</td><td>` + esc(p.Repository) + `</td><td>` + state + `</td></tr>`
	}
	body += `</table>`
	renderPage(w, "下载", body)
}

func (s Server) statsPage(w http.ResponseWriter, _ *http.Request) {
	renderPage(w, "统计数据", `<p class="muted">统计尚未启用。M5 将实现下载授权次数、开始传输授权数、每日流量、累计流量和趋势聚合。</p>`)
}

func (s Server) aboutPage(w http.ResponseWriter, _ *http.Request) {
	renderPage(w, "关于", `<p>本服务提供公开 GitHub Release 文件镜像下载。</p><p class="muted">镜像内容来自公开仓库，本站不是 GitHub 官方服务。网页下载使用自托管 ALTCHA，公开 API 使用独立 SHA-256 前导零 PoW。</p>`)
}

func (s Server) nodesPage(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.Store.Nodes(r.Context())
	if err != nil {
		http.Error(w, "节点状态读取失败", http.StatusInternalServerError)
		return
	}
	body := `<p class="muted">SLA 和历史可用率尚未启用。</p><table><tr><th>节点</th><th>状态</th><th>同步就绪</th><th>最近心跳</th></tr>`
	for _, n := range nodes {
		ready := "否"
		if n.RoutingReady {
			ready = "是"
		}
		body += `<tr><td>` + esc(n.PublicName) + `</td><td>` + esc(n.State) +
			`</td><td>` + ready + `</td><td>` + esc(n.LastHeartbeat) + `</td></tr>`
	}
	body += `</table>`
	renderPage(w, "节点状态", body)
}

func renderPage(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTemplate.Execute(w, pageData{Title: title, Body: template.HTML(body)})
}

func esc(value string) string {
	return template.HTMLEscapeString(value)
}
