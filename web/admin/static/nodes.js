(function () {
  var a = window.admin;
  if (!a || a.page() !== "nodes") return;
  var currentNode = "";
  var nodeCache = {};

  function nodeCard(nodeID) {
    var cards = document.querySelectorAll("[data-node-card]");
    for (var i = 0; i < cards.length; i++) {
      if (cards[i].getAttribute("data-node-card") === nodeID) return cards[i];
    }
    return null;
  }

  function connectionLabel(node) {
    var state = String(node.connection_state || node.state || "").toLowerCase();
    if (state === "offline") return "离线";
    if (state === "disabled") return "已禁用";
    return "在线";
  }

  function readinessLabel(node) {
    var state = String(node.connection_state || node.state || "").toLowerCase();
    if (state === "offline" || state === "disabled") return "";
    return node.routing_ready ? "全量就绪" : "同步准备中";
  }

  function renderNodes(nodes) {
    var list = document.getElementById("nodes-list");
    nodeCache = {};
    if (!nodes || !nodes.length) {
      list.innerHTML = '<div class="admin-record muted">暂无节点</div>';
      currentNode = "";
      a.text("node-summary", "0 个节点");
      return;
    }
    list.innerHTML = nodes.map(function (node) {
      nodeCache[node.node_id] = node;
      var ready = readinessLabel(node);
      var open = currentNode === node.node_id;
      var meta = a.regionLabel(node.region) + " · Control " + (node.control_protocol || "未知") +
        " · 压力 " + a.pressureText(node) + " · 心跳 " + (node.last_heartbeat_at || "暂无");
      return '<article class="admin-record node-record" data-node-card="' + a.esc(node.node_id) + '">' +
        '<div class="admin-record__summary"><div class="admin-record__identity"><strong>' +
        a.esc(node.public_name || node.node_id) + '</strong><span class="sub">' + a.esc(meta) +
        '</span></div><div class="admin-record__state">' + a.badge(connectionLabel(node)) +
        (ready ? a.badge(ready) : "") + '<span class="admin-record__meta">' +
        a.esc(a.bandwidthText(node)) + '</span></div><div class="admin-record__actions">' +
        '<button class="admin-secondary" type="button" data-node-action="detail" data-node="' +
        a.esc(node.node_id) + '" aria-expanded="' + open + '">' + (open ? "收起" : "查看详情") +
        '</button></div></div><div class="admin-record__detail" data-node-detail' +
        (open ? "" : " hidden") + '></div></article>';
    }).join("");
    a.text("node-summary", nodes.length + " 个节点");
    if (currentNode && nodeCache[currentNode]) renderDetail(currentNode);
    if (currentNode && !nodeCache[currentNode]) currentNode = "";
  }

  function syncHeadline(sync) {
    var state = String(sync.connection_state || "").toLowerCase();
    if (sync.error) return "状态读取失败";
    if (state === "offline") return "节点离线";
    if (state === "disabled") return "节点已禁用";
    if (Number(sync.failed_tasks || 0) > 0) return "同步失败";
    if (Number(sync.mismatched_assets || 0) > 0) return "文件校验异常";
    return a.syncPhaseLabel(sync.sync_phase);
  }

  function reasonClass(sync) {
    if (sync.error || sync.connection_state === "offline" || Number(sync.failed_tasks || 0) > 0 ||
        Number(sync.mismatched_assets || 0) > 0) return " admin-record__message--bad";
    if (sync.sync_phase === "retry_wait") return " admin-record__message--warn";
    return "";
  }

  function renderDetail(nodeID) {
    var card = nodeCard(nodeID);
    if (!card) return;
    var box = card.querySelector("[data-node-detail]");
    box.hidden = false;
    box.innerHTML = '<div class="muted">正在读取节点详情...</div>';
    Promise.all([
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/sync-status").catch(function (err) { return { error: err.message }; }),
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/reports").catch(function (err) { return { error: err.message }; }),
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/sla").catch(function (err) { return { error: err.message }; })
    ]).then(function (items) {
      if (currentNode !== nodeID) return;
      card = nodeCard(nodeID);
      if (!card) return;
      box = card.querySelector("[data-node-detail]");
      var sync = items[0], reports = items[1], sla = items[2];
      var node = nodeCache[nodeID] || {};
      var pressure = reports.pressure || reports.heartbeat || {};
      var assetFS = pressure.asset_fs || {};
      var partialFS = pressure.partial_fs || {};
      var free = Number(pressure.free_bytes || 0) > 0 ? a.bytes(pressure.free_bytes) : "节点暂未上报";
      var assetFree = assetFS.valid ? a.bytes(Math.max(0, Number(assetFS.available_bytes || 0) - Number(assetFS.reserved_bytes || 0))) : free;
      var partialFree = partialFS.valid ? a.bytes(Math.max(0, Number(partialFS.available_bytes || 0) - Number(partialFS.reserved_bytes || 0))) : "节点暂未上报";
      var publicActive = pressure.public_active_downloads !== undefined ? pressure.public_active_downloads : (pressure.active_downloads || 0);
      var slaText = (sla.windows || []).map(function (w) {
        return w.window + " " + (w.insufficient_samples ? "样本不足" :
          (Number(w.availability_ratio || 0) * 100).toFixed(2) + "%");
      }).join(" · ");
      var reason = sync.error || (sync.routing_ready ? "" : (sync.routing_ready_reason || ""));
      box.innerHTML = '<div class="admin-record__state"><strong>同步状态</strong>' +
        a.badge(syncHeadline(sync)) + '</div>' +
        (reason ? '<p class="admin-record__message' + reasonClass(sync) + '">' + a.esc(reason) + '</p>' : "") +
        '<div class="detail-split"><section class="detail-pane"><h3>同步概况</h3>' +
        a.compactKv({"目标文件": sync.required_assets || 0, "已验证": sync.verified_assets || 0,
          "待同步": sync.missing_assets || 0, "执行中": sync.running_tasks || 0,
          "等待重试": sync.retry_wait_tasks || 0, "失败任务": sync.failed_tasks || 0}, "detail-plain") +
        '</section><section class="detail-pane"><h3>运行概况</h3>' +
        a.compactKv({"Control": node.control_protocol || "未知", "当前带宽": a.bandwidthText(node),
          "压力": a.pressureText(node), "资产盘可用": assetFree, "Partial 盘可用": partialFree,
          "公网活动下载": publicActive, "Swarm 活动上传": pressure.swarm_active_uploads || 0,
          "最近报告": pressure.reported_at || "暂无", "地区": a.regionLabel(node.region)}, "detail-plain") +
        '</section></div><div><h3>SLA</h3><p class="muted">' + a.esc(slaText || "暂无样本") + '</p></div>' +
        '<div class="admin-record__actions admin-record__management">' +
        '<a class="admin-secondary admin-link-button" href="/admin/nodes/' + encodeURIComponent(nodeID) + '/management">管理项目与调度</a>' +
        '<button class="admin-secondary" data-node-action="sync-reset" data-node="' + a.esc(nodeID) + '">重置同步</button>' +
        '<button class="admin-secondary" data-node-action="' + (node.state === "disabled" ? "enable" : "disable") +
        '" data-node="' + a.esc(nodeID) + '">' + (node.state === "disabled" ? "启用节点" : "禁用节点") + '</button>' +
        '<button class="admin-secondary admin-danger" data-node-action="delete" data-node="' + a.esc(nodeID) + '">删除节点</button></div>' +
        '<details class="admin-tech-details"><summary>显示技术信息</summary>' +
        a.compactKv({"Node ID": nodeID, "软件版本": node.software_version || "未知",
          "路由详情": sync.routing_ready_detail || "", "库存 revision": sync.latest_inventory_revision || 0}, "detail-plain") +
        '</details>';
    });
  }

  function toggleDetail(nodeID) {
    var previous = currentNode;
    currentNode = previous === nodeID ? "" : nodeID;
    if (previous) {
      var old = nodeCard(previous);
      if (old) {
        old.querySelector("[data-node-detail]").hidden = true;
        var oldButton = old.querySelector('[data-node-action="detail"]');
        if (oldButton) { oldButton.textContent = "查看详情"; oldButton.setAttribute("aria-expanded", "false"); }
      }
    }
    if (!currentNode) return;
    var card = nodeCard(nodeID);
    if (card) {
      var button = card.querySelector('[data-node-action="detail"]');
      if (button) { button.textContent = "收起"; button.setAttribute("aria-expanded", "true"); }
    }
    renderDetail(nodeID);
  }

  function nodeAction(button) {
    var nodeID = button.getAttribute("data-node");
    var action = button.getAttribute("data-node-action");
    if (action === "detail") return toggleDetail(nodeID);
    var node = nodeCache[nodeID] || {};
    var name = node.public_name || nodeID;
    var labels = {"sync-reset": "重新开始同步", disable: "禁用", enable: "启用", delete: "删除"};
    var message = action === "delete" ? "确认删除节点「" + name + "」？节点运行数据、任务、库存和授权记录会被清理。"
      : action === "sync-reset" ? "确认重新开始「" + name + "」的同步？将停止旧任务并重新拉取缺失文件，保留已验证文件和分片。"
      : "确认对「" + name + "」执行“" + labels[action] + "”？";
    a.confirmAction("节点操作", message, function () {
      var path = "/admin/api/nodes/" + encodeURIComponent(nodeID);
      var options = {method: "DELETE", body: "{}"};
      if (action !== "delete") { path += "/" + action; options.method = "POST"; }
      a.api(path, options).then(function (data) {
        a.setStatus(data.message || "操作已完成");
        if (action === "delete") currentNode = "";
        loadNodes();
      }).catch(function (err) { a.setStatus(err.message); });
    });
  }

  function loadNodes() {
    return a.api("/admin/api/nodes").then(function (data) {
      renderNodes(data.nodes || []);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-node-action]");
    if (button) nodeAction(button);
  });
  var pairing = document.getElementById("pairing-create");
  if (pairing) pairing.addEventListener("click", function () {
    a.confirmAction("创建配对码", "确认创建一个 5 分钟有效的一次性配对码？", function () {
      a.api("/admin/api/pairing-codes", {method: "POST", body: JSON.stringify({ttl_seconds: 300})})
        .then(function (data) { a.infoDialog("配对码已创建", "过期时间：" + data.expires_at, data.pairing_code); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  });
  loadNodes();
  a.autoRefresh(loadNodes, 10000);
})();
