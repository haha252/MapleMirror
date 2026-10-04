(function () {
  const chart = document.getElementById("stats-chart");
  const tooltip = document.getElementById("stats-tooltip");
  const metrics = document.getElementById("stats-metrics");
  const ranks = document.getElementById("stats-ranks");
  const nodes = document.getElementById("stats-nodes");
  if (!chart || !tooltip) return;
  const i18n = window.MirrorI18n;
  const text = (key, fallback, params) => i18n ? i18n.t(key, params) : fallback;
  const source = (value, fallback) => i18n ? i18n.sourceLabel(value) : fallback;
  let data = JSON.parse(chart.dataset.trends || "[]");
  let metricData = null;
  let sourceData = null;
  let rankData = null;
  let nodeData = null;
  const metricNames = [["stats.views", "stats.visits", false], ["stats.downloads", "stats.downloadCount", false], ["stats.traffic", "", true]];
  const sourceTools = window.MirrorStatsSources;
  const fmt = window.MirrorStatsMetrics.formatNumber;
  const bytes = window.MirrorStatsMetrics.formatBytes;
  const esc = window.MirrorStatsMetrics.escape;
  function stateText(value) {
    const state = String(value || "").toLowerCase();
    if (state === "online" || state === "syncing" || state === "ready") return text("stats.state.online", "在线");
    if (state === "offline") return text("stats.state.offline", "离线");
    if (state === "disabled") return text("stats.state.disabled", "已禁用");
    if (state === "pending") return text("stats.state.pending", "待接入");
    return value || "";
  }
  function displayTime(value) {
    if (!value) return "";
    return i18n ? i18n.formatDate(value) : value;
  }
  function detail(key, value, fallback) {
    return value ? '<span class="sub">' + esc(text(key, fallback || key)) +
      esc(text("stats.detailSeparator", "：")) + esc(value) + "</span>" : "";
  }
  const chartView = window.MirrorStatsChart.create(chart, tooltip, {line: "views"});
  function renderMetrics(items, sources) {
    if (!metrics || !Array.isArray(items)) return;
    window.MirrorStatsMetrics.render(metrics, metricNames.map((meta, i) => ({
      title: meta[0], unit: meta[1], traffic: meta[2], values: items[i],
      sources: i === 1 ? sources : null
    })));
  }
  function renderRanks(items) {
    if (!ranks || !Array.isArray(items)) return;
    if (!items.length) {
      ranks.innerHTML = '<p class="muted empty">' + esc(text("stats.noDownloads", "暂无下载数据")) + "</p>";
      return;
    }
    ranks.innerHTML = items.map((row, i) => {
      const badge = i < 3 ? "rank-badge" : "rank-badge rank-badge--muted";
      return '<div class="rank-item"><span class="' + badge + '"><span>' + (i + 1) +
        '</span></span><div><strong>' + esc(row[0]) + '</strong><span class="rank-source">' +
        source("web", "Web") + " " + fmt(row[2] || 0) + " / " +
        source("api", "API") + " " + fmt(row[3] || 0) +
        '</span></div><b>' + fmt(row[1] || 0) + '</b></div>';
    }).join("");
  }
  function renderNodes(items) {
    if (!nodes || !Array.isArray(items)) return;
    let html = '<div class="node-table panel-card"><div class="node-table__scroll"><table>' +
      '<tr><th>' + text("stats.nodeName", "节点名称") + '</th><th>' + text("stats.state", "状态") +
      '</th><th>' + text("stats.pressure", "压力") + '</th><th>' + text("stats.sla24", "24小时 SLA") + '</th>' +
      '<th>' + text("stats.sla7", "7天 SLA") + '</th><th>' + text("stats.totalTraffic", "总下载流量") + '</th></tr>';
    items.forEach((row) => {
      const ready = Number(row[3]) === 1 ? detail("stats.downloadReady", text("stats.yes", "是")) :
        detail("stats.downloadReady", text("stats.no", "否：{value}", {value: row[4] || ""}));
      html += "<tr><td>" + esc(row[0]) + detail("stats.recentHeartbeat", displayTime(row[2])) +
        "</td><td>" + esc(stateText(row[1])) + ready + "</td><td>" +
        esc(row[8] || text("stats.none", "暂无")) + "</td><td>" + esc(row[5]) + "</td><td>" +
        esc(row[6]) + "</td><td>" + bytes(row[7]) + "</td></tr>";
    });
    nodes.innerHTML = html + "</table></div></div>";
  }
  function mergeTodayPoint(item) {
    if (!item) return;
    const index = data.findIndex((row) => row.day === item.day);
    if (index >= 0) {
      data[index] = item;
    } else {
      data.push(item);
      data = data.slice(-30);
    }
  }
  function scheduleRender() { chartView.update(data); }
  async function refreshFast() {
    try {
      const res = await fetch("/api/public/v1/stats", {cache: "no-store"});
      if (!res.ok) return;
      const snapshot = await res.json();
      metricData = snapshot.m;
      sourceData = snapshot.ds;
      renderMetrics(metricData, sourceData);
      mergeTodayPoint(sourceTools.todayPoint(snapshot.p));
      chart.dataset.trends = JSON.stringify(data);
      scheduleRender();
    } catch (_) {}
  }
  async function refreshDetails() {
    try {
      const res = await fetch("/api/public/v1/stats/details", {cache: "no-store"});
      if (!res.ok) return;
      const snapshot = await res.json();
      rankData = snapshot.r;
      nodeData = snapshot.n;
      renderRanks(rankData);
      renderNodes(nodeData);
      data = sourceTools.compactTrend(snapshot.t);
      chart.dataset.trends = JSON.stringify(data);
      scheduleRender();
    } catch (_) {}
  }
  scheduleRender();
  function refreshAll() { refreshDetails(); refreshFast(); }
  if (i18n) i18n.onChange(function () {
    renderMetrics(metricData, sourceData);
    renderRanks(rankData);
    renderNodes(nodeData);
    scheduleRender();
  });
  refreshAll();
  window.setInterval(function () { if (!document.hidden) refreshFast(); }, 10000);
  window.setInterval(function () { if (!document.hidden) refreshDetails(); }, 60000);
  document.addEventListener("visibilitychange", function () { if (!document.hidden) refreshAll(); });
})();
