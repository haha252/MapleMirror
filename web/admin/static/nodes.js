(function () {
  var a = window.admin;
  if (!a || a.page() !== "nodes") return;
  var currentNode = "";

  function renderNodes(nodes) {
    var body = document.getElementById("nodes-body");
    if (!body) return;
    if (!nodes || !nodes.length) {
      body.innerHTML = '<tr><td colspan="5" class="muted">暂无节点</td></tr>';
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
        a.badge(node.routing_ready ? "全量就绪" : "未全量就绪") + "</td><td>" +
        a.esc(node.last_heartbeat_at || "暂无") + '</td><td><div class="admin-actions">' +
        '<button class="admin-secondary" type="button" data-node-action="detail" data-node="' +
        a.esc(node.node_id) + '" aria-expanded="' + (node.node_id === currentNode ? "true" : "false") + '">详情</button>' +
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
      row.innerHTML = '<td colspan="5"><div class="admin-inline-detail detail-stack"></div></td>';
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
      var slaText = (sla.windows || []).map(function (w) {
        var pct = (Number(w.availability_ratio || 0) * 100).toFixed(2) + "%";
        return w.window + ": " + (w.insufficient_samples ? "样本不足" : pct);
      }).join(" / ");
      if (currentNode !== nodeID) return;
      box = detailBox(nodeID);
      if (!box) return;
      box.innerHTML =
        '<h3>同步诊断</h3>' + a.kv({
          "同步阶段": sync.sync_phase || sync.error || "未知",
          "必需资产": sync.required_assets || 0,
          "已验证资产": sync.verified_assets || 0,
          "缺失资产": sync.missing_assets || 0,
          "失败任务": sync.failed_tasks || 0,
          "就绪原因": sync.routing_ready_reason || ""
        }) + '<h3>最近报告</h3>' + a.kv({
          "心跳": (reports.heartbeat && reports.heartbeat.reported_at) || "暂无",
          "库存": (reports.inventory && reports.inventory.reported_at) || "暂无",
          "压力": (reports.pressure && reports.pressure.reported_at) || "暂无"
        }) + '<h3>SLA</h3><p class="muted">' + a.esc(slaText || "暂无样本") + "</p>";
    });
  }

  function loadNodes() {
    return a.api("/admin/api/nodes").then(function (data) {
      renderNodes(data.nodes || []);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function loadPairing() {
    return a.api("/admin/api/pairing-requests").then(function (data) {
      var body = document.getElementById("pairing-body");
      if (!body) return;
      var rows = data.requests || [];
      if (!rows.length) {
        body.innerHTML = '<tr><td colspan="4" class="muted">暂无待审批登记</td></tr>';
        return;
      }
      body.innerHTML = rows.map(function (item) {
        var name = item.PublicName || item.public_name || "";
        var fp = item.Fingerprint || item.fingerprint || "";
        var id = item.ID || item.id;
        return "<tr><td>" + a.esc(name) + "</td><td>" + a.esc(fp) +
          "</td><td>" + a.esc(item.ExpiresAt || item.expires_at || "") +
          '</td><td><div class="admin-actions"><button class="admin-secondary" data-pairing-action="approve" data-name="' +
          a.esc(name) + '" data-fingerprint="' + a.esc(fp) + '" data-id="' + a.esc(id) +
          '">批准</button><button class="admin-secondary" data-pairing-action="reject" data-id="' +
          a.esc(id) + '">拒绝</button></div></td></tr>';
      }).join("");
    }).catch(function (err) { a.setStatus(err.message); });
  }

  document.addEventListener("click", function (event) {
    var nodeButton = event.target.closest("[data-node-action]");
    if (nodeButton) return nodeAction(nodeButton);
    var pairButton = event.target.closest("[data-pairing-action]");
    if (pairButton) return pairingAction(pairButton);
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

  function pairingAction(button) {
    var id = button.getAttribute("data-id");
    var action = button.getAttribute("data-pairing-action");
    a.confirmAction("登记请求", "确认" + (action === "approve" ? "批准" : "拒绝") + "该登记请求？", function () {
      var payload = action === "approve" ? {
        confirmed_public_name: button.getAttribute("data-name"),
        confirmed_public_key_fingerprint: button.getAttribute("data-fingerprint")
      } : {};
      a.api("/admin/api/pairing-requests/" + encodeURIComponent(id) + "/" + action, {
        method: "POST", body: JSON.stringify(payload)
      }).then(function (data) { a.setStatus(data.message || "操作已完成"); loadPairing(); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  document.getElementById("pairing-create").addEventListener("click", function () {
    a.confirmAction("创建配对码", "确认创建一个 5 分钟有效的一次性配对码？", function () {
      a.api("/admin/api/pairing-codes", { method: "POST", body: JSON.stringify({ ttl_seconds: 300 }) })
        .then(function (data) {
          var box = document.getElementById("pairing-code");
          box.hidden = false;
          box.textContent = "配对码：" + data.pairing_code + "，过期时间：" + data.expires_at;
        }).catch(function (err) { a.setStatus(err.message); });
    });
  });

  loadNodes();
  loadPairing();
})();
