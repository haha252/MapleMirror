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
  let frame = 0;
  let metricData = null;
  let sourceData = null;
  let rankData = null;
  let nodeData = null;
  const metricNames = [["stats.views", "stats.visits", false], ["stats.downloads", "stats.downloadCount", false], ["stats.traffic", "", true]];
  const sourceTools = window.MirrorStatsSources;
  function fmt(value) {
    return i18n ? i18n.formatNumber(value) : new Intl.NumberFormat(document.documentElement.lang || undefined).format(value || 0);
  }
  function bytes(value) {
    if (value < 1024 * 1024) return fmt(value) + " B";
    if (value < 1024 * 1024 * 1024) return (value / 1024 / 1024).toFixed(2) + " MiB";
    if (value < 1024 * 1024 * 1024 * 1024) return (value / 1024 / 1024 / 1024).toFixed(2) + " GiB";
    return (value / 1024 / 1024 / 1024 / 1024).toFixed(2) + " TiB";
  }
  function esc(value) {
    return String(value || "").replace(/[&<>"']/g, function (char) {
      return {"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"}[char];
    });
  }
  function trendLabel(recent, previous) {
    if (!previous) return recent ? "+100%" : "0%";
    const change = ((recent - previous) / previous) * 100;
    return (change < 0 ? "" : "+") + change.toFixed(1) + "%";
  }
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
  function moveTooltip(event) {
    const rect = chart.getBoundingClientRect();
    const leftHalf = event.clientX < rect.left + rect.width / 2;
    const tipW = tooltip.offsetWidth || 160;
    const tipH = tooltip.offsetHeight || 0;
    const x = leftHalf ? event.clientX - tipW - 14 : event.clientX + 14;
    const y = event.clientY - tipH - 14;
    tooltip.style.left = Math.max(8, Math.min(x, window.innerWidth - tipW - 8)) + "px";
    tooltip.style.top = Math.max(8, Math.min(y, window.innerHeight - tipH - 8)) + "px";
  }
  function smoothPath(points) {
    if (points.length < 2) return "";
    let out = "M " + points[0].join(" ");
    for (let i = 1; i < points.length; i++) {
      const prev = points[i - 1], cur = points[i];
      const midX = (prev[0] + cur[0]) / 2;
      out += " C " + midX + " " + prev[1] + ", " + midX + " " + cur[1] + ", " + cur[0] + " " + cur[1];
    }
    return out;
  }
  function chartSize() {
    const rect = chart.getBoundingClientRect();
    return {width: Math.max(320, Math.round(rect.width || 320)), height: Math.max(220, Math.round(rect.height || 220))};
  }
  function render() {
    tooltip.hidden = true;
    if (!data.length) {
      chart.innerHTML = '<p class="muted">' + esc(text("stats.noTrend", "暂无趋势数据")) + "</p>";
      return;
    }
    const size = chartSize();
    const width = size.width, height = size.height;
    const pad = {left: width < 520 ? 50 : 70, right: 18, top: 18, bottom: 36};
    const plotW = width - pad.left - pad.right;
    const plotH = height - pad.top - pad.bottom;
    const maxValue = Math.max(...data.map((item) => {
      const parts = sourceTools.sourceParts(item);
      return Math.max(item.views || 0, parts.total);
    }));
    const maxY = maxValue > 0 ? maxValue / 0.95 : 1;
    const step = plotW / Math.max(data.length - 1, 1);
    const barW = Math.max(5, Math.min(24, step * 0.58));
    const x = (i) => pad.left + i * step;
    const y = (value) => pad.top + plotH - (value || 0) * plotH / maxY;
    let html = '<svg viewBox="0 0 ' + width + " " + height + '" role="img" aria-label="' +
      esc(text("stats.chartAria", "下载趋势")) + '">';
    for (let i = 0; i <= 4; i++) {
      const value = Math.round(maxY * (4 - i) / 4);
      const gy = pad.top + plotH * i / 4;
      html += '<line class="chart-grid" x1="' + pad.left + '" x2="' + (width - pad.right) + '" y1="' + gy + '" y2="' + gy + '"/>';
      html += '<text class="chart-label" x="' + (pad.left - 10) + '" y="' + (gy + 4) + '" text-anchor="end">' + fmt(value) + "</text>";
    }
    html += '<line class="chart-axis" x1="' + pad.left + '" x2="' + pad.left + '" y1="' + pad.top + '" y2="' + (pad.top + plotH) + '"/>';
    html += '<line class="chart-axis" x1="' + pad.left + '" x2="' + (width - pad.right) + '" y1="' + (pad.top + plotH) + '" y2="' + (pad.top + plotH) + '"/>';
    data.forEach((item, i) => {
      const parts = sourceTools.sourceParts(item);
      const scale = plotH / maxY;
      const webH = Math.max(0, parts.web * scale);
      const apiH = Math.max(0, parts.api * scale);
      const bottom = pad.top + plotH;
      html += '<rect class="chart-bar" x="' + (x(i) - barW / 2) + '" y="' + (bottom - webH) +
        '" width="' + barW + '" height="' + webH + '"/>';
      html += '<rect class="chart-bar chart-bar--api" x="' + (x(i) - barW / 2) + '" y="' + (bottom - webH - apiH) +
        '" width="' + barW + '" height="' + apiH + '"/>';
    });
    html += '<path class="chart-line" d="' + smoothPath(data.map((item, i) => [x(i), y(item.views)])) + '"/>';
    data.forEach((item, i) => {
      if (i % 5 === 0 || i === data.length - 1) {
        html += '<text class="chart-label" x="' + x(i) + '" y="' + (height - 8) + '" text-anchor="middle">' + item.day.slice(5) + "</text>";
      }
      html += '<rect class="chart-hit" data-index="' + i + '" x="' + (x(i) - step / 2) +
        '" y="0" width="' + Math.max(step, 18) + '" height="' + height + '"/>';
    });
    chart.innerHTML = html + "</svg>";
    chart.querySelectorAll(".chart-hit").forEach(bindTooltip);
  }
  function bindTooltip(hit) {
    hit.addEventListener("mousemove", function (event) {
      const item = data[Number(hit.dataset.index)];
      const parts = sourceTools.sourceParts(item);
      tooltip.hidden = false;
      tooltip.innerHTML = "<b>" + item.day.slice(5) + "</b><br>" +
        esc(text("stats.viewsTooltip", "访问量")) + " " + fmt(item.views) +
        "<br>" + esc(text("stats.downloadsTooltip", "下载量")) + " " + fmt(parts.total) +
        "<br>" + esc(source("web", "Web 下载")) + " " + fmt(parts.web) +
        "<br>" + esc(source("api", "API 下载")) + " " + fmt(parts.api) +
        "<br>" + esc(text("stats.traffic", "总流量")) + " " + bytes(item.sent_bytes);
      moveTooltip(event);
    });
    hit.addEventListener("mouseleave", function () { tooltip.hidden = true; });
  }
  function renderMetrics(items, sources) {
    if (!metrics || !Array.isArray(items)) return;
    metrics.innerHTML = items.map((row, i) => {
      const meta = metricNames[i];
      const value = meta[2] ? bytes(row[0]) : fmt(row[0]);
      const recent = meta[2] ? bytes(row[1]) : fmt(row[1]) + " " + text(meta[1], meta[1]);
      const label = trendLabel(row[1], row[2]);
      const trendClass = label[0] === "-" ? "trend-down" : "trend-up";
      const breakdown = i === 1 ? '<div class="metric-breakdown"><span>' + source("web", "Web") + " " +
        fmt(((sources || [])[0] || [])[1] || 0) + '</span><span>' + source("api", "API") + " " +
        fmt(((sources || [])[1] || [])[1] || 0) + '</span></div>' : "";
      const cardClass = i === 1 ? "metric-card metric-card--sources panel-card" : "metric-card panel-card";
      return '<article class="' + cardClass + '"><div class="metric-card__top"><h3>' + text(meta[0], meta[0]) +
        '</h3><span class="' + trendClass + '">' + label + '</span></div><strong>' +
        value + '</strong><p class="muted">' + text("stats.recent30", "近 30 日 {value}", {value: recent}) + '</p>' + breakdown + '</article>';
    }).join("");
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
  function scheduleRender() { window.cancelAnimationFrame(frame); frame = window.requestAnimationFrame(render); }
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
  render();
  if ("ResizeObserver" in window) {
    new ResizeObserver(scheduleRender).observe(chart);
  } else {
    window.addEventListener("resize", scheduleRender);
  }
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
