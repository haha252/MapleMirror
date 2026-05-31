(function () {
  const chart = document.getElementById("stats-chart");
  const tooltip = document.getElementById("stats-tooltip");
  if (!chart || !tooltip) return;
  const data = JSON.parse(chart.dataset.trends || "[]");
  if (!data.length) {
    chart.innerHTML = '<p class="muted">暂无趋势数据</p>';
    return;
  }

  function fmt(value) {
    return new Intl.NumberFormat("zh-CN").format(value || 0);
  }

  function bytes(value) {
    if (value < 1024 * 1024) return fmt(value) + " B";
    if (value < 1024 * 1024 * 1024) return (value / 1024 / 1024).toFixed(2) + " MiB";
    if (value < 1024 * 1024 * 1024 * 1024) return (value / 1024 / 1024 / 1024).toFixed(2) + " GiB";
    return (value / 1024 / 1024 / 1024 / 1024).toFixed(2) + " TiB";
  }

  function moveTooltip(event) {
    const rect = chart.getBoundingClientRect();
    const leftHalf = event.clientX < rect.left + rect.width / 2;
    const tipW = tooltip.offsetWidth || 160;
    const x = leftHalf ? event.clientX - tipW - 14 : event.clientX + 14;
    tooltip.style.left = Math.max(8, Math.min(x, window.innerWidth - tipW - 8)) + "px";
    tooltip.style.top = Math.max(8, event.clientY - tooltip.offsetHeight - 14) + "px";
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

  const width = 900;
  const height = 310;
  const pad = {left: 70, right: 18, top: 18, bottom: 36};
  const plotW = width - pad.left - pad.right;
  const plotH = height - pad.top - pad.bottom;
  const maxValue = Math.max(...data.map((item) => Math.max(item.views || 0, item.downloads || 0)));
  const maxY = maxValue > 0 ? maxValue / 0.95 : 1;
  const step = plotW / Math.max(data.length - 1, 1);
  const barW = Math.max(6, Math.min(24, step * 0.58));
  const x = (i) => pad.left + i * step;
  const y = (value) => pad.top + plotH - (value || 0) * plotH / maxY;

  let html = '<svg viewBox="0 0 ' + width + " " + height + '" role="img" aria-label="下载趋势">';
  for (let i = 0; i <= 4; i++) {
    const value = Math.round(maxY * (4 - i) / 4);
    const gy = pad.top + plotH * i / 4;
    html += '<line class="chart-grid" x1="' + pad.left + '" x2="' + (width - pad.right) + '" y1="' + gy + '" y2="' + gy + '"/>';
    html += '<text class="chart-label" x="' + (pad.left - 10) + '" y="' + (gy + 4) + '" text-anchor="end">' + fmt(value) + "</text>";
  }
  html += '<line class="chart-axis" x1="' + pad.left + '" x2="' + pad.left + '" y1="' + pad.top + '" y2="' + (pad.top + plotH) + '"/>';
  html += '<line class="chart-axis" x1="' + pad.left + '" x2="' + (width - pad.right) + '" y1="' + (pad.top + plotH) + '" y2="' + (pad.top + plotH) + '"/>';
  data.forEach((item, i) => {
    const h = plotH - (y(item.downloads) - pad.top);
    html += '<rect class="chart-bar" x="' + (x(i) - barW / 2) + '" y="' + (pad.top + plotH - h) + '" width="' + barW + '" height="' + h + '"/>';
  });
  html += '<path class="chart-line" d="' + smoothPath(data.map((item, i) => [x(i), y(item.views)])) + '"/>';
  data.forEach((item, i) => {
    if (i % 5 === 0 || i === data.length - 1) {
      html += '<text class="chart-label" x="' + x(i) + '" y="' + (height - 8) + '" text-anchor="middle">' + item.day.slice(5) + "</text>";
    }
    html += '<rect class="chart-hit" data-index="' + i + '" x="' + (x(i) - step / 2) + '" y="0" width="' + Math.max(step, 18) + '" height="' + height + '"/>';
  });
  html += "</svg>";
  chart.innerHTML = html;

  chart.querySelectorAll(".chart-hit").forEach((hit) => {
    hit.addEventListener("mousemove", function (event) {
      const item = data[Number(hit.dataset.index)];
      tooltip.hidden = false;
      tooltip.innerHTML = "<b>" + item.day.slice(5) + "</b><br>访问量 " + fmt(item.views) +
        "<br>下载量 " + fmt(item.downloads) + "<br>流量 " + bytes(item.sent_bytes);
      moveTooltip(event);
    });
    hit.addEventListener("mouseleave", function () { tooltip.hidden = true; });
  });
})();
