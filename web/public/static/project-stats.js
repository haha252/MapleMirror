(function () {
  const page = document.querySelector(".project-page[data-project-id]");
  if (!page) return;
  const projectID = page.dataset.projectId;
  const metrics = document.getElementById("project-stats-metrics");
  const chart = document.getElementById("project-stats-chart");
  const tooltip = document.getElementById("project-stats-tooltip");
  const status = document.getElementById("project-stats-status");
  const retry = document.getElementById("project-stats-retry");
  const legend = document.getElementById("project-stats-legend");
  const tools = window.MirrorStatsMetrics;
  const text = tools.text;
  const chartView = window.MirrorStatsChart.create(chart, tooltip, {line: "none"});
  let snapshot = null, busy = false, failed = false, view = "downloads";
  function render() {
    status.removeAttribute("data-i18n");
    status.textContent = failed ? text(snapshot ? "project.stats.refreshFailed" : "project.stats.failed", "统计读取失败，请重试") :
      snapshot ? snapshot.has_data ? "" : text("stats.noDownloads", "暂无下载数据") : text("stats.loading", "正在加载统计数据...");
    retry.hidden = !failed;
    status.parentElement.hidden = !status.textContent && !failed;
    if (!snapshot) {
      metrics.querySelectorAll("p").forEach((item) => {
        item.removeAttribute("data-i18n");
        item.textContent = text(failed ? "project.stats.unavailable" : "stats.reading", "正在读取数据");
      });
      chart.innerHTML = '<p class="muted">' + tools.escape(text(failed ? "project.stats.failed" : "stats.loading", "正在加载统计数据...")) + '</p>';
      return;
    }
    window.MirrorStatsMetrics.render(metrics, [
      ["downloads", "stats.downloads"], ["web_downloads", "project.stats.web"],
      ["api_downloads", "project.stats.api"], ["traffic", "stats.traffic"]
    ].map(([key, title]) => {
      const item = snapshot.metrics[key];
      return {title: title, unit: "stats.downloadCount", traffic: key === "traffic", values: [item.total, item.recent, item.previous]};
    }));
    legend.innerHTML = view === "traffic" ? '<span class="project-stats-legend__total">' +
      tools.escape(text("stats.traffic", "总流量")) + '</span>' :
      '<span class="project-stats-legend__web">' + tools.escape(text("stats.webDownload", "网页下载")) +
      '</span><span class="project-stats-legend__api">' + tools.escape(text("stats.apiDownload", "API 下载")) + '</span>';
    chartView.update(snapshot.trend, view);
  }
  async function refresh() {
    if (busy || document.hidden) return;
    busy = true;
    retry.disabled = true;
    const controller = typeof AbortController === "function" ? new AbortController() : null;
    let timeout;
    try {
      const request = fetch("/api/public/v1/projects/" + encodeURIComponent(projectID) + "/stats", {
        cache: "no-store", signal: controller ? controller.signal : undefined
      }).then((res) => {
        if (!res.ok) throw new Error("stats unavailable");
        return res.json();
      });
      const deadline = new Promise((resolve, reject) => {
        timeout = window.setTimeout(() => {
          if (controller) controller.abort();
          reject(new Error("stats timeout"));
        }, 15000);
      });
      const payload = await Promise.race([request, deadline]);
      const next = payload.data;
      if (payload.status !== "success" || !next || next.project_id !== projectID ||
        !next.metrics || !Array.isArray(next.trend) ||
        ["downloads", "web_downloads", "api_downloads", "traffic"].some((key) => !next.metrics[key])) throw new Error("invalid project stats");
      snapshot = next;
      failed = false;
    } catch (_) {
      failed = true;
    } finally {
      window.clearTimeout(timeout);
      busy = false;
      retry.disabled = false;
      metrics.setAttribute("aria-busy", "false");
      chart.setAttribute("aria-busy", "false");
      render();
    }
  }
  page.querySelectorAll("[data-stats-view]").forEach((button) => {
    button.addEventListener("click", function () {
      view = button.dataset.statsView;
      page.querySelectorAll("[data-stats-view]").forEach((item) => item.setAttribute("aria-pressed", String(item === button)));
      render();
    });
  });
  retry.addEventListener("click", refresh);
  if (window.MirrorI18n) window.MirrorI18n.onChange(render);
  document.addEventListener("visibilitychange", function () { if (!document.hidden) refresh(); });
  window.setInterval(refresh, 10000);
  render();
  refresh();
})();
