(function () {
  var a = window.admin;
  if (!a || a.page() !== "overview") return;

  function renderNodeStats(stats) {
    var box = document.getElementById("node-summary-grid");
    if (!box) return;
    var rows = [
      ["在线", stats.online || 0],
      ["离线", stats.offline || 0],
      ["已禁用", stats.disabled || 0],
      ["路由就绪", stats.routing_ready || 0]
    ];
    box.innerHTML = rows.map(function (row) {
      return '<div class="summary-item"><span>' + a.esc(row[0]) +
        '</span><strong>' + a.esc(row[1]) + "</strong></div>";
    }).join("");
  }

  function renderScanAlerts(scans) {
    var body = document.getElementById("scan-alerts-body");
    if (!body) return;
    var alerts = (scans || []).filter(function (item) {
      return item.last_scan_state === "failed" || item.last_error_message;
    });
    if (!alerts.length) {
      body.innerHTML = '<tr><td colspan="3" class="muted">暂无扫描异常</td></tr>';
      return;
    }
    body.innerHTML = alerts.map(function (item) {
      return "<tr><td><strong>" + a.esc(item.project_id) + "</strong></td><td>" +
        a.badge(item.last_scan_state || "未知") + "</td><td>" +
        a.esc(item.last_error_message || "无错误摘要") + "</td></tr>";
    }).join("");
  }

  a.api("/admin/api/overview").then(function (data) {
    a.text("metric-auth", data.stats.authorization_count || 0);
    a.text("metric-started", data.stats.transfer_started_count || 0);
    a.text("metric-daily", a.bytes(data.stats.daily_sent_bytes));
    a.text("metric-total", a.bytes(data.stats.total_sent_bytes));
    renderNodeStats(data.node_stat || {});
    renderScanAlerts(data.scans || []);
    a.setStatus("");
  }).catch(function (err) {
    a.setStatus(err.message || "管理总览加载失败");
  });
})();
