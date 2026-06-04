package public

import (
	"encoding/json"
	"html/template"
	"time"
)

func statsBody(stats StatsDashboard, nodes []NodeSummary) template.HTML {
	body := `<section class="stats-section"><h2>总计信息</h2><div class="metric-grid">`
	body += metricCard("总访问量", stats.TotalViews, num(stats.TotalViews.Total), "近 30 日 "+num(stats.TotalViews.Recent)+" 次访问")
	body += metricCard("总下载量", stats.TotalDownloads, num(stats.TotalDownloads.Total), "近 30 日 "+num(stats.TotalDownloads.Recent)+" 次下载")
	body += metricCard("总流量", stats.TotalTraffic, bytesText(stats.TotalTraffic.Total), "近 30 日 "+bytesText(stats.TotalTraffic.Recent))
	body += `</div><div class="stats-layout"><section class="panel-card rank-card"><h3>热门资源排行</h3><p class="muted">下载量最高的项目版本</p><div class="rank-list">`
	for i, item := range stats.Resources {
		body += rankItem(i+1, item)
	}
	if len(stats.Resources) == 0 {
		body += `<p class="muted empty">暂无下载数据</p>`
	}
	body += `</div></section>` + trendChart(stats.Trend) + `</div></section>`
	body += `<section class="stats-section"><h2>节点信息</h2>` + nodesTable(nodes) + `</section>`
	return template.HTML(body)
}

func metricCard(title string, metric MetricStat, value, sub string) string {
	trendClass := "trend-up"
	if len(metric.TrendLabel) > 0 && metric.TrendLabel[0] == '-' {
		trendClass = "trend-down"
	}
	return `<article class="metric-card panel-card"><div class="metric-card__top"><h3>` + esc(title) +
		`</h3><span class="` + trendClass + `">` + esc(metric.TrendLabel) + `</span></div><strong>` +
		esc(value) + `</strong><p class="muted">` + esc(sub) + `</p></article>`
}

func rankItem(rank int, item ResourceRank) string {
	badge := "rank-badge"
	if rank > 3 {
		badge += " rank-badge--muted"
	}
	return `<div class="rank-item"><span class="` + badge + `"><span>` + num(int64(rank)) +
		`</span></span><div><strong>` + esc(item.ProjectName) + `</strong><span>` +
		esc(item.Version+" "+item.Architecture) + `</span></div><b>` +
		num(item.DownloadCount) + `</b></div>`
}

func trendChart(trends []DailyTrend) string {
	data, _ := json.Marshal(trends)
	return `<section class="panel-card chart-card"><div class="chart-card__head"><h3>下载趋势</h3><p class="muted">最近 30 天访问量与下载量变化</p></div><div id="stats-chart" class="stats-chart" data-trends='` +
		template.HTMLEscapeString(string(data)) + `'></div><div id="stats-tooltip" class="stats-tooltip" hidden></div></section>`
}

func nodesTable(nodes []NodeSummary) string {
	body := `<div class="node-table panel-card"><table><tr><th>节点名称</th><th>状态</th><th>24小时 SLA</th><th>7天 SLA</th><th>总下载流量</th></tr>`
	for _, n := range nodes {
		body += `<tr><td>` + esc(n.PublicName) + renderDetail("最近心跳", displayTime(n.LastHeartbeat)) +
			`</td><td>` + esc(stateText(n.State)) + downloadDetail(n) + `</td><td>` +
			esc(n.SLA24H) + `</td><td>` + esc(n.SLA7D) + `</td><td>` +
			bytesText(n.TotalSentBytes) + `</td></tr>`
	}
	body += `</table></div>`
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
		return renderDetail("下载就绪", "是")
	}
	return renderDetail("下载就绪", "否 "+n.DownloadReadyReason)
}
