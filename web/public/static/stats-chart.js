(function () {
  const tools = window.MirrorStatsMetrics;
  const text = tools.text, esc = tools.escape, fmt = tools.formatNumber, bytes = tools.formatBytes;
  function smoothPath(points) {
    if (!points.length) return "";
    let out = "M " + points[0].join(" ");
    for (let i = 1; i < points.length; i++) {
      const prev = points[i - 1], cur = points[i], midX = (prev[0] + cur[0]) / 2;
      out += " C " + midX + " " + prev[1] + ", " + midX + " " + cur[1] + ", " + cur[0] + " " + cur[1];
    }
    return out;
  }
  function create(chart, tooltip, options) {
    let data = [], frame = 0, traffic = false;
    const lineValue = (item) => traffic ? item.sent_bytes : options.line === "views" ? item.views : item.downloads;
    function moveTooltip(clientX, clientY) {
      const tipW = tooltip.offsetWidth || 160, tipH = tooltip.offsetHeight || 0;
      const rect = chart.getBoundingClientRect();
      const x = clientX < rect.left + rect.width / 2 ? clientX + 14 : clientX - tipW - 14;
      tooltip.style.left = Math.max(8, Math.min(x, window.innerWidth - tipW - 8)) + "px";
      tooltip.style.top = Math.max(8, Math.min(clientY - tipH - 14, window.innerHeight - tipH - 8)) + "px";
    }
    function show(hit, event) {
      const item = data[Number(hit.dataset.index)];
      const parts = window.MirrorStatsSources.sourceParts(item);
      tooltip.hidden = false;
      tooltip.innerHTML = "<b>" + esc(item.day) + "</b>" + (options.line === "views" ? "<br>" +
        esc(text("stats.viewsTooltip", "访问量")) + " " + fmt(item.views) : "") +
        "<br>" + esc(text("stats.downloadsTooltip", "下载量")) + " " + fmt(parts.total) +
        "<br>" + esc(text("stats.webDownload", "网页下载")) + " " + fmt(parts.web) +
        "<br>" + esc(text("stats.apiDownload", "API 下载")) + " " + fmt(parts.api) +
        "<br>" + esc(text("stats.traffic", "总流量")) + " " + bytes(item.sent_bytes);
      const rect = hit.getBoundingClientRect();
      moveTooltip(event && event.clientX != null ? event.clientX : rect.left + rect.width / 2,
        event && event.clientY != null ? event.clientY : rect.top + rect.height / 2);
    }
    function bindTooltip(hit) {
      hit.addEventListener("mousemove", (event) => show(hit, event));
      hit.addEventListener("mouseleave", () => { tooltip.hidden = true; });
      hit.addEventListener("click", (event) => show(hit, event));
      hit.addEventListener("focus", () => show(hit));
      hit.addEventListener("blur", () => { tooltip.hidden = true; });
      hit.addEventListener("keydown", function (event) {
        if (event.key === "Escape") { tooltip.hidden = true; return; }
        if (event.key === "Enter" || event.key === " ") { event.preventDefault(); show(hit); return; }
        const current = Number(hit.dataset.index);
        const index = event.key === "ArrowLeft" ? Math.max(0, current - 1) :
          event.key === "ArrowRight" ? Math.min(data.length - 1, current + 1) :
          event.key === "Home" ? 0 : event.key === "End" ? data.length - 1 : null;
        if (index == null) return;
        event.preventDefault();
        hit.setAttribute("tabindex", "-1");
        const next = chart.querySelector('[data-index="' + index + '"]');
        next.setAttribute("tabindex", "0");
        next.focus();
      });
    }
    function render() {
      const active = document.activeElement;
      const focusedIndex = chart.contains(active) && active.dataset.index != null ? Number(active.dataset.index) : null;
      tooltip.hidden = true;
      if (!data.length) {
        chart.innerHTML = '<p class="muted">' + esc(text("stats.noTrend", "暂无趋势数据")) + "</p>";
        return;
      }
      const rect = chart.getBoundingClientRect();
      const width = Math.max(240, Math.round(rect.width || 320)), height = Math.max(220, Math.round(rect.height || 260));
      const pad = {left: traffic ? 88 : width < 520 ? 50 : 70, right: 18, top: 18, bottom: 36};
      const plotW = width - pad.left - pad.right, plotH = height - pad.top - pad.bottom;
      const maxValue = Math.max(...data.map((item) => Math.max(lineValue(item) || 0,
        traffic ? 0 : window.MirrorStatsSources.sourceParts(item).total)));
      const maxY = maxValue > 0 ? maxValue / 0.95 : 1;
      const step = plotW / Math.max(data.length - 1, 1), barW = Math.max(3, Math.min(24, step * 0.58));
      const x = (i) => pad.left + i * step;
      const y = (value) => pad.top + plotH - (value || 0) * plotH / maxY;
      const tabIndex = focusedIndex == null ? data.length - 1 : Math.min(focusedIndex, data.length - 1);
      let html = '<svg viewBox="0 0 ' + width + " " + height + '" role="group" aria-label="' +
        esc(text(traffic ? "project.stats.trafficTrend" : "stats.chartAria", "下载趋势")) + '">';
      for (let i = 0; i <= 4; i++) {
        const value = Math.round(maxY * (4 - i) / 4), gy = pad.top + plotH * i / 4;
        html += '<line class="chart-grid" x1="' + pad.left + '" x2="' + (width - pad.right) + '" y1="' + gy + '" y2="' + gy + '"/>';
        html += '<text class="chart-label" x="' + (pad.left - 10) + '" y="' + (gy + 4) + '" text-anchor="end">' +
          esc(traffic ? bytes(value) : fmt(value)) + "</text>";
      }
      html += '<line class="chart-axis" x1="' + pad.left + '" x2="' + pad.left + '" y1="' + pad.top + '" y2="' + (pad.top + plotH) + '"/>';
      html += '<line class="chart-axis" x1="' + pad.left + '" x2="' + (width - pad.right) + '" y1="' + (pad.top + plotH) + '" y2="' + (pad.top + plotH) + '"/>';
      if (!traffic) data.forEach((item, i) => {
        const parts = window.MirrorStatsSources.sourceParts(item), scale = plotH / maxY;
        const webH = Math.max(0, parts.web * scale), apiH = Math.max(0, parts.api * scale), bottom = pad.top + plotH;
        html += '<rect class="chart-bar" x="' + (x(i) - barW / 2) + '" y="' + (bottom - webH) +
          '" width="' + barW + '" height="' + webH + '"/>';
        html += '<rect class="chart-bar chart-bar--api" x="' + (x(i) - barW / 2) + '" y="' + (bottom - webH - apiH) +
          '" width="' + barW + '" height="' + apiH + '"/>';
      });
      if (traffic || options.line !== "none") {
        html += '<path class="chart-line" d="' + smoothPath(data.map((item, i) => [x(i), y(lineValue(item))])) + '"/>';
      }
      data.forEach((item, i) => {
        if (i % (width < 400 ? 10 : 5) === 0 || i === data.length - 1) {
          html += '<text class="chart-label" x="' + x(i) + '" y="' + (height - 8) + '" text-anchor="middle">' + esc(item.day.slice(5)) + "</text>";
        }
        const parts = window.MirrorStatsSources.sourceParts(item);
        const label = item.day + ", " + text("stats.downloadsTooltip", "下载量") + " " + fmt(parts.total) + ", " +
          text("stats.webDownload", "网页下载") + " " + fmt(parts.web) + ", " + text("stats.apiDownload", "API 下载") +
          " " + fmt(parts.api) + ", " + text("stats.traffic", "总流量") + " " + bytes(item.sent_bytes);
        html += '<rect class="chart-hit" role="button" tabindex="' + (i === tabIndex ? "0" : "-1") + '" aria-label="' + esc(label) +
          '" data-index="' + i + '" x="' + (x(i) - step / 2) + '" y="0" width="' + Math.max(step, 8) + '" height="' + height + '"/>';
      });
      chart.innerHTML = html + "</svg>";
      chart.querySelectorAll(".chart-hit").forEach(bindTooltip);
      if (focusedIndex != null) chart.querySelector('[data-index="' + tabIndex + '"]').focus({preventScroll: true});
    }
    function scheduleRender() { window.cancelAnimationFrame(frame); frame = window.requestAnimationFrame(render); }
    if ("ResizeObserver" in window) new ResizeObserver(scheduleRender).observe(chart);
    else window.addEventListener("resize", scheduleRender);
    return {update: function (points, view) {
      data = points || [];
      traffic = view === "traffic";
      scheduleRender();
    }};
  }
  window.MirrorStatsChart = {create: create};
})();
