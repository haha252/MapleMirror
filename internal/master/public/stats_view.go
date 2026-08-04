package public

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"
)

func statsShellBody() template.HTML {
	body := `<section class="stats-section"><h2 data-i18n="stats.total">总计信息</h2><div id="stats-metrics" class="metric-grid">` +
		metricPlaceholder("总访问量", "stats.views") + metricPlaceholder("总下载量", "stats.downloads") + metricPlaceholder("总流量", "stats.traffic") + `</div>`
	body += `<div class="stats-layout"><section class="panel-card rank-card"><h3 data-i18n="stats.popular">热门资源排行</h3><p class="muted" data-i18n="stats.popularDescription">下载量最高的项目</p><div id="stats-ranks" class="rank-list"><p class="muted empty" data-i18n="stats.loading">正在加载统计数据...</p></div></section>` +
		`<section class="panel-card chart-card"><div class="chart-card__head"><h3 data-i18n="stats.trend">下载趋势</h3><p class="muted" data-i18n="stats.trendDescription">最近 30 天访问量与 Web/API 下载量变化</p></div><div id="stats-chart" class="stats-chart" data-trends="[]"></div><div id="stats-tooltip" class="stats-tooltip" hidden></div></section></div></section>`
	body += `<section class="stats-section"><h2 data-i18n="stats.nodes">节点信息</h2><div id="stats-nodes"><p class="muted empty" data-i18n="stats.loadingNodes">正在加载节点状态...</p></div></section>`
	return template.HTML(body)
}

func metricPlaceholder(title, key string) string {
	return `<article class="metric-card panel-card"><div class="metric-card__top"><h3 data-i18n="` + key + `">` +
		esc(title) + `</h3><span class="muted" data-i18n="stats.loadingState">加载中</span></div><strong>--</strong><p class="muted" data-i18n="stats.reading">正在读取数据</p></article>`
}

func statsBody(stats StatsDashboard, nodes []NodeSummary) template.HTML {
	body := `<section class="stats-section"><h2 data-i18n="stats.total">总计信息</h2>` + metricsGrid(stats)
	body += `<div class="stats-layout"><section class="panel-card rank-card"><h3 data-i18n="stats.popular">热门资源排行</h3><p class="muted" data-i18n="stats.popularDescription">下载量最高的项目</p>` +
		rankList(stats.Projects) + `</section>` + trendChart(stats.Trend) + `</div></section>`
	body += `<section class="stats-section"><h2 data-i18n="stats.nodes">节点信息</h2><div id="stats-nodes">` + nodesTable(nodes) + `</div></section>`
	return template.HTML(body)
}

func metricsGrid(stats StatsDashboard) string {
	body := `<div id="stats-metrics" class="metric-grid">`
	body += metricCard("总访问量", stats.TotalViews, numComma(stats.TotalViews.Total), "近 30 日 "+numComma(stats.TotalViews.Recent)+" 次访问")
	body += metricCardWithBreakdown("总下载量", stats.TotalDownloads,
		numComma(stats.TotalDownloads.Total), "近 30 日 "+numComma(stats.TotalDownloads.Recent)+" 次下载",
		stats.DownloadSources.Web, stats.DownloadSources.API)
	body += metricCard("总流量", stats.TotalTraffic, bytesText(stats.TotalTraffic.Total), "近 30 日 "+bytesText(stats.TotalTraffic.Recent))
	return body + `</div>`
}

func rankList(items []ProjectRank) string {
	body := `<div id="stats-ranks" class="rank-list">`
	for i, item := range items {
		body += rankItem(i+1, item)
	}
	if len(items) == 0 {
		body += `<p class="muted empty" data-i18n="stats.noDownloads">暂无下载数据</p>`
	}
	return body + `</div>`
}

func metricCard(title string, metric MetricStat, value, sub string) string {
	trendClass := "trend-up"
	if len(metric.TrendLabel) > 0 && metric.TrendLabel[0] == '-' {
		trendClass = "trend-down"
	}
	return `<article class="metric-card panel-card"><div class="metric-card__top"><h3 data-i18n="` + statsMetricKey(title) + `">` + esc(title) +
		`</h3><span class="` + trendClass + `">` + esc(metric.TrendLabel) + `</span></div><strong>` +
		esc(value) + `</strong><p class="muted">` + esc(sub) + `</p></article>`
}

func statsMetricKey(title string) string {
	switch title {
	case "总访问量":
		return "stats.views"
	case "总下载量":
		return "stats.downloads"
	case "总流量":
		return "stats.traffic"
	default:
		return ""
	}
}

func metricCardWithBreakdown(title string, metric MetricStat, value, sub string, web, api MetricStat) string {
	body := metricCard(title, metric, value, sub)
	breakdown := `<div class="metric-breakdown"><span>Web ` + numComma(web.Recent) +
		`</span><span>API ` + numComma(api.Recent) + `</span></div>`
	body = strings.Replace(body, `metric-card panel-card`, `metric-card metric-card--sources panel-card`, 1)
	return strings.Replace(body, `</article>`, breakdown+`</article>`, 1)
}

func rankItem(rank int, item ProjectRank) string {
	badge := "rank-badge"
	if rank > 3 {
		badge += " rank-badge--muted"
	}
	return `<div class="rank-item"><span class="` + badge + `"><span>` + numComma(int64(rank)) +
		`</span></span><div><strong>` + esc(item.ProjectName) + `</strong><span class="rank-source">` +
		`Web ` + numComma(item.WebDownloadCount) + ` / API ` + numComma(item.APIDownloadCount) +
		`</span></div><b>` + numComma(item.DownloadCount) + `</b></div>`
}

func trendChart(trends []DailyTrend) string {
	data, _ := json.Marshal(trends)
	return `<section class="panel-card chart-card"><div class="chart-card__head">` +
		`<h3 data-i18n="stats.trend">下载趋势</h3><p class="muted" data-i18n="stats.trendDescription">最近 30 天访问量与 Web/API 下载量变化</p></div>` +
		`<div id="stats-chart" class="stats-chart" data-trends='` +
		template.HTMLEscapeString(string(data)) + `'></div><div id="stats-tooltip" class="stats-tooltip" hidden></div></section>`
}

func nodesTable(nodes []NodeSummary) string {
	body := `<div class="node-table panel-card"><div class="node-table__scroll"><table>` +
		`<tr><th data-i18n="stats.nodeName">节点名称</th><th data-i18n="stats.state">状态</th><th data-i18n="stats.pressure">压力</th><th data-i18n="stats.sla24">24小时 SLA</th>` +
		`<th data-i18n="stats.sla7">7天 SLA</th><th data-i18n="stats.totalTraffic">总下载流量</th></tr>`
	for _, n := range nodes {
		body += `<tr><td>` + esc(n.PublicName) + renderDetail("最近心跳", displayTime(n.LastHeartbeat)) +
			`</td><td>` + stateHTML(n.State) + downloadDetail(n) + `</td><td>` +
			esc(n.PressureRatio) + `</td><td>` + esc(n.SLA24H) + `</td><td>` + esc(n.SLA7D) + `</td><td>` +
			bytesText(n.TotalSentBytes) + `</td></tr>`
	}
	body += `</table></div></div>`
	return body
}

func displayTime(value string) string {
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.In(time.Local).Format("2006/01/02 15:04")
	}
	return value
}

func downloadDetail(n NodeSummary) string {
	if n.DownloadReady {
		return `<span class="sub">下载就绪：是</span>`
	}
	return `<span class="sub">下载就绪：否：` + esc(n.DownloadReadyReason) + `</span>`
}

func numComma(value int64) string {
	s := template.HTMLEscapeString(strings.TrimSpace(fmt.Sprintf("%d", value)))
	n := len(s)
	if n <= 3 {
		return s
	}
	var out strings.Builder
	rem := n % 3
	if rem == 0 {
		rem = 3
	}
	out.WriteString(s[:rem])
	for i := rem; i < n; i += 3 {
		out.WriteByte(',')
		out.WriteString(s[i : i+3])
	}
	return out.String()
}
