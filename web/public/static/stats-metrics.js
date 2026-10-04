(function () {
  const i18n = window.MirrorI18n;
  function text(key, fallback, params) {
    if (i18n) return i18n.t(key, params);
    return Object.keys(params || {}).reduce((value, name) => value.replace("{" + name + "}", params[name]), fallback);
  }
  function escape(value) {
    return String(value == null ? "" : value).replace(/[&<>"']/g, function (char) {
      return {"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"}[char];
    });
  }
  function formatNumber(value) {
    return i18n ? i18n.formatNumber(value) : new Intl.NumberFormat(document.documentElement.lang || undefined).format(value || 0);
  }
  function formatBytes(value) {
    let size = Number(value || 0), unit = 0;
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
    while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
    return (unit ? size.toFixed(2) : formatNumber(size)) + " " + units[unit];
  }
  function source(value) {
    return i18n ? i18n.sourceLabel(value) : value === "api" ? "API" : "Web";
  }
  function trendLabel(recent, previous) {
    if (!previous) return recent ? "+100%" : "0%";
    const change = ((recent - previous) / previous) * 100;
    return (change < 0 ? "" : "+") + change.toFixed(1) + "%";
  }
  function render(container, definitions) {
    container.innerHTML = definitions.map((item) => {
      const row = item.values || [0, 0, 0];
      const value = item.traffic ? formatBytes(row[0]) : formatNumber(row[0]);
      const recent = item.traffic ? formatBytes(row[1]) : formatNumber(row[1]) + " " + text(item.unit, "次下载");
      const label = trendLabel(row[1], row[2]);
      const trendClass = label[0] === "-" ? "trend-down" : "trend-up";
      const breakdown = item.sources ? '<div class="metric-breakdown"><span>' + escape(source("web")) + " " +
        formatNumber((item.sources[0] || [])[1] || 0) + '</span><span>' + escape(source("api")) + " " +
        formatNumber((item.sources[1] || [])[1] || 0) + '</span></div>' : "";
      return '<article class="metric-card panel-card' + (item.sources ? " metric-card--sources" : "") +
        '"><div class="metric-card__top"><h3>' + escape(text(item.title, item.title)) +
        '</h3><span class="' + trendClass + '" title="' + escape(text("project.stats.comparison", "较前一个 30 日窗口")) +
        '">' + label + '</span></div><strong>' + value + '</strong><p class="muted">' +
        escape(text("stats.recent30", "近 30 日 {value}", {value: recent})) + '</p>' + breakdown + '</article>';
    }).join("");
  }
  window.MirrorStatsMetrics = {render: render, formatNumber: formatNumber, formatBytes: formatBytes,
    escape: escape, text: text, source: source};
})();
