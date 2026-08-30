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
        '</span><strong>' + a.esc(row[1]) + '</strong></div>';
    }).join("");
  }

  function attentionCard(title, subtitle, state, href, action) {
    return '<article class="admin-record"><div class="admin-record__summary">' +
      '<div class="admin-record__identity"><strong>' + a.esc(title) +
      '</strong><span class="sub">' + a.esc(subtitle) + '</span></div>' +
      '<div class="admin-record__state">' + a.badge(state) + '</div>' +
      '<div class="admin-record__actions"><a class="admin-secondary admin-link-button" href="' +
      a.esc(href) + '">' + a.esc(action) + '</a></div></div></article>';
  }

  function renderAttention(nodes, scans) {
    var list = document.getElementById("overview-attention");
    var cards = [];
    (nodes || []).forEach(function (node) {
      var state = String(node.connection_state || node.state || "").toLowerCase();
      if (state !== "offline") return;
      cards.push(attentionCard(node.public_name || node.node_id,
        "最近心跳 " + (node.last_heartbeat_at || "暂无"), "离线", "/admin/nodes", "查看节点"));
    });
    (scans || []).forEach(function (scan) {
      if (scan.last_scan_state !== "failed" && !scan.last_error_message) return;
      cards.push(attentionCard(scan.project_name || scan.project_id,
        scan.last_error_message || "上次扫描失败", "扫描失败", "/admin/sync", "查看同步"));
    });
    list.innerHTML = cards.length ? cards.join("") :
      '<div class="security-attention-empty">当前没有需要处理的问题。</div>';
  }

  function loadOverview() {
    return a.api("/admin/api/overview").then(function (data) {
      a.text("metric-auth", data.stats.authorization_count || 0);
      a.text("metric-started", data.stats.transfer_started_count || 0);
      a.text("metric-daily", a.bytes(data.stats.daily_sent_bytes));
      a.text("metric-total", a.bytes(data.stats.total_sent_bytes));
      renderNodeStats(data.node_stat || {});
      renderAttention(data.nodes || [], data.scans || []);
      a.setStatus("");
    }).catch(function (err) {
      a.setStatus(err.message || "管理总览加载失败");
    });
  }

  var indexNowButton = document.getElementById("indexnow-submit");
  if (indexNowButton) {
    indexNowButton.addEventListener("click", function () {
      a.confirmAction("立即提交 IndexNow", "确认立即提交全量公开 URL？", function () {
        indexNowButton.disabled = true;
        a.api("/admin/api/indexnow/submit", {method: "POST", body: "{}"})
          .then(function (data) {
            a.setStatus((data.message || "IndexNow 全量 URL 已排队") +
              "（新增 " + (data.url_count || 0) + " 条）");
          })
          .catch(function (err) { a.setStatus(err.message); })
          .then(function () { indexNowButton.disabled = false; });
      });
    });
  }

  loadOverview();
  a.autoRefresh(loadOverview, 15000);
})();
