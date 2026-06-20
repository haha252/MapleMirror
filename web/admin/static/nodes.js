(function () {
  var a = window.admin;
  if (!a || a.page() !== "nodes") return;
  var currentNode = "";

  function routeLabel(node) {
    var state = String(node.connection_state || node.state || "").toLowerCase();
    if (state === "offline" || state === "disabled") return "不路由";
    return node.routing_ready ? "全量就绪" : "未全量就绪";
  }

  function renderNodes(nodes) {
    var body = document.getElementById("nodes-body");
    if (!body) return;
    if (!nodes || !nodes.length) {
      body.innerHTML = '<tr><td colspan="8" class="muted">暂无节点</td></tr>';
      currentNode = "";
      return;
    }
    var hasCurrent = false;
    body.innerHTML = nodes.map(function (node) {
      var state = node.connection_state || node.state;
      if (node.node_id === currentNode) hasCurrent = true;
      return '<tr data-node-row="' + a.esc(node.node_id) + '"><td><strong>' +
        a.esc(node.public_name || node.node_id) +
        '</strong><span class="sub">' + a.esc(node.node_id) + "</span></td><td>" +
        a.badge(a.connectionLabel(state)) + "</td><td>" +
        a.badge(routeLabel(node)) + "</td><td>" +
        '<span class="metric-inline">' + a.esc(a.bandwidthText(node)) + "</span></td><td>" +
        a.pressureMeter(node) + "</td><td>" +
        '<span data-node-priority-value="' + a.esc(node.node_id) + '">' +
        a.esc(node.download_priority == null ? 50 : node.download_priority) + "</span></td><td>" +
        a.esc(node.last_heartbeat_at || "暂无") + '</td><td><div class="admin-actions">' +
        '<button class="admin-secondary" type="button" data-node-action="detail" data-node="' +
        a.esc(node.node_id) + '" aria-expanded="' + (node.node_id === currentNode ? "true" : "false") + '">详情</button>' +
        '<a class="admin-secondary admin-link-button" href="/admin/nodes/' + encodeURIComponent(node.node_id) +
        '/management">管理</a>' +
        '<button class="admin-secondary" data-node-action="sync-reset" data-node="' + a.esc(node.node_id) + '">重置</button>' +
        '<button class="admin-secondary" data-node-action="' + (node.state === "disabled" ? "enable" : "disable") +
        '" data-node="' + a.esc(node.node_id) + '">' + (node.state === "disabled" ? "启用" : "禁用") +
        '</button><button class="admin-secondary admin-danger" data-node-action="delete" data-node="' +
        a.esc(node.node_id) + '">删除</button></div></td></tr>';
    }).join("");
    a.text("node-summary", nodes.length + " 个节点");
    if (currentNode && hasCurrent) {
      renderDetail(currentNode);
    } else {
      currentNode = "";
    }
  }

  function detailRow() {
    return document.querySelector("[data-node-detail-row]");
  }

  function nodeRow(nodeID) {
    var rows = document.querySelectorAll("[data-node-row]");
    for (var i = 0; i < rows.length; i++) {
      if (rows[i].getAttribute("data-node-row") === nodeID) return rows[i];
    }
    return null;
  }

  function setDetailButtons() {
    document.querySelectorAll('[data-node-action="detail"]').forEach(function (button) {
      button.setAttribute("aria-expanded", button.getAttribute("data-node") === currentNode ? "true" : "false");
    });
  }

  function clearDetail() {
    var row = detailRow();
    if (row) row.remove();
    setDetailButtons();
  }

  function detailBox(nodeID, html) {
    var row = detailRow();
    if (!row || row.getAttribute("data-node-detail-row") !== nodeID) {
      clearDetail();
      var anchor = nodeRow(nodeID);
      if (!anchor) return null;
      row = document.createElement("tr");
      row.className = "admin-inline-detail-row";
      row.setAttribute("data-node-detail-row", nodeID);
      row.innerHTML = '<td colspan="8"><div class="admin-inline-detail detail-stack"></div></td>';
      anchor.insertAdjacentElement("afterend", row);
    }
    var box = row.querySelector(".admin-inline-detail");
    if (box && html != null) box.innerHTML = html;
    return box;
  }

  function toggleDetail(nodeID) {
    if (currentNode === nodeID) {
      currentNode = "";
      clearDetail();
      return;
    }
    currentNode = nodeID;
    renderDetail(nodeID);
  }

  function renderDetail(nodeID) {
    var box = detailBox(nodeID, '<div class="muted">加载中...</div>');
    setDetailButtons();
    if (!box) return;
    Promise.all([
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/sync-status").catch(function (err) { return { error: err.message }; }),
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/reports").catch(function (err) { return { error: err.message }; }),
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/sla").catch(function (err) { return { error: err.message }; })
    ]).then(function (items) {
      var sync = items[0], reports = items[1], sla = items[2];
      var pressure = reports.pressure || reports.heartbeat || {};
      var slaText = (sla.windows || []).map(function (w) {
        var pct = (Number(w.availability_ratio || 0) * 100).toFixed(2) + "%";
        return w.window + ": " + (w.insufficient_samples ? "样本不足" : pct);
      }).join(" / ");
      if (currentNode !== nodeID) return;
      box = detailBox(nodeID);
      if (!box) return;
      box.innerHTML =
        '<div class="detail-split">' +
        '<section class="detail-pane"><h3>同步诊断</h3>' + a.compactKv({
          "同步阶段": sync.sync_phase || sync.error || "未知",
          "必需资产": sync.required_assets || 0,
          "已验证资产": sync.verified_assets || 0,
          "缺失资产": sync.missing_assets || 0,
          "失败任务": sync.failed_tasks || 0,
          "就绪原因": sync.routing_ready_reason || ""
        }, "detail-plain") + '</section>' +
        '<section class="detail-pane"><h3>最近报告</h3>' + a.compactKv({
          "心跳": (reports.heartbeat && reports.heartbeat.reported_at) || "暂无",
          "库存": (reports.inventory && reports.inventory.reported_at) || "暂无",
          "压力": pressure.reported_at || "暂无",
          "目标带宽": pressure.target_bandwidth_bps ? a.bytes(pressure.target_bandwidth_bps) + "/s" : "未上报",
          "实际带宽": pressure.actual_bandwidth_bps ? a.bytes(pressure.actual_bandwidth_bps) + "/s" : "暂无采样",
          "压力比": pressure.pressure_ratio != null ? (Number(pressure.pressure_ratio) * 100).toFixed(1) + "%" : "暂无"
        }, "detail-plain") + '</section></div><h3>SLA</h3><p class="muted">' + a.esc(slaText || "暂无样本") +
        '</p>';
    });
  }

  function loadNodes() {
    return a.api("/admin/api/nodes").then(function (data) {
      renderNodes(data.nodes || []);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  document.addEventListener("click", function (event) {
    var nodeButton = event.target.closest("[data-node-action]");
    if (nodeButton) return nodeAction(nodeButton);
  });

  function nodeAction(button) {
    var node = button.getAttribute("data-node");
    var action = button.getAttribute("data-node-action");
    if (action === "detail") return toggleDetail(node);
    var labels = { "sync-reset": "重置同步状态", "disable": "禁用", "enable": "启用", "delete": "删除" };
    var text = action === "delete" ? "确认删除节点 " + node + "？该操作会清理该节点的运行数据、任务、库存和授权记录。"
      : "确认对节点 " + node + " 执行 " + labels[action] + "？";
    a.confirmAction("节点操作", text, function () {
      var path = "/admin/api/nodes/" + encodeURIComponent(node);
      var options = { method: "DELETE", body: "{}" };
      if (action !== "delete") {
        path += "/" + action;
        options.method = "POST";
      }
      a.api(path, options)
        .then(function (data) {
          a.setStatus(data.message || "操作已完成");
          if (action === "delete") {
            if (currentNode === node) {
              currentNode = "";
              clearDetail();
            }
            return loadNodes();
          }
          currentNode = node;
          loadNodes();
        })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  var pairingCreate = document.getElementById("pairing-create");
  if (pairingCreate) {
    pairingCreate.addEventListener("click", function () {
      a.confirmAction("创建配对码", "确认创建一个 5 分钟有效的一次性配对码？", function () {
        a.api("/admin/api/pairing-codes", { method: "POST", body: JSON.stringify({ ttl_seconds: 300 }) })
          .then(function (data) {
            a.infoDialog("配对码已创建", "过期时间：" + data.expires_at, data.pairing_code);
          }).catch(function (err) { a.setStatus(err.message); });
      });
    });
  }

  loadNodes();
  a.autoRefresh(loadNodes, 3000);
})();
