(function () {
  var status = document.getElementById("admin-status");

  function setStatus(text) {
    if (!status) return;
    status.textContent = text;
    status.hidden = !text;
  }

  function text(id, value) {
    var el = document.getElementById(id);
    if (el) el.textContent = value;
  }

  function bytes(value) {
    var units = ["B", "KiB", "MiB", "GiB", "TiB"];
    var n = Number(value) || 0;
    var index = 0;
    while (n >= 1024 && index < units.length - 1) {
      n = n / 1024;
      index++;
    }
    return (index === 0 ? String(n) : n.toFixed(1)) + " " + units[index];
  }

  function esc(value) {
    return String(value == null ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function badge(value) {
    return '<span class="admin-badge">' + esc(value || "未知") + "</span>";
  }

  function renderNodes(nodes) {
    var body = document.getElementById("nodes-body");
    if (!body) return;
    body.innerHTML = (nodes || []).map(function (node) {
      return "<tr><td><strong>" + esc(node.public_name || node.node_id) +
        '</strong><span class="sub">' + esc(node.node_id) + "</span></td><td>" +
        badge(node.state) + "</td><td>" + badge(node.routing_ready ? "可路由" : "不可路由") +
        "</td><td>" + esc(node.last_heartbeat_at || "暂无") + "</td></tr>";
    }).join("");
    text("node-summary", (nodes || []).length + " 个节点");
  }

  function renderScans(scans) {
    var body = document.getElementById("scans-body");
    if (!body) return;
    body.innerHTML = (scans || []).map(function (scan) {
      return "<tr><td><strong>" + esc(scan.project_id) + "</strong></td><td>" +
        badge(scan.last_scan_state || "未扫描") + "</td><td>" +
        esc(scan.next_scan_at || "暂无") + "</td></tr>";
    }).join("");
    text("scan-summary", (scans || []).length + " 个项目");
  }

  function loadProjects() {
    return fetch("/admin/api/projects", { credentials: "same-origin" })
      .then(function (res) {
        if (!res.ok) throw new Error("项目配置加载失败");
        return res.json();
      })
      .then(function (data) {
        var editor = document.getElementById("projects-editor");
        if (editor) editor.value = JSON.stringify(data, null, 2);
      });
  }

  function saveProjects() {
    var editor = document.getElementById("projects-editor");
    if (!editor) return;
    var data;
    try {
      data = JSON.parse(editor.value);
    } catch (err) {
      setStatus("项目配置 JSON 不合法，请检查逗号、引号和括号。");
      return;
    }
    setStatus("正在保存项目配置...");
    fetch("/admin/api/projects", {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data)
    }).then(function (res) {
      return res.json().then(function (body) {
        if (!res.ok) throw new Error(body.message || "项目配置保存失败");
        return body;
      });
    }).then(function () {
      setStatus("项目配置已保存，并已同步运行状态。");
      return loadProjects();
    }).catch(function (err) {
      setStatus(err.message || "项目配置保存失败");
    });
  }

  var saveButton = document.getElementById("projects-save");
  if (saveButton) saveButton.addEventListener("click", saveProjects);

  fetch("/admin/api/overview", { credentials: "same-origin" })
    .then(function (res) {
      if (!res.ok) throw new Error("管理总览加载失败");
      return res.json();
    })
    .then(function (data) {
      text("metric-auth", data.stats.authorization_count || 0);
      text("metric-started", data.stats.transfer_started_count || 0);
      text("metric-daily", bytes(data.stats.daily_sent_bytes));
      text("metric-total", bytes(data.stats.total_sent_bytes));
      renderNodes(data.nodes);
      renderScans(data.scans);
      setStatus("");
      return loadProjects();
    })
    .catch(function () {
      setStatus("管理总览加载失败，请检查会话、管理网络和服务日志。");
    });
})();
