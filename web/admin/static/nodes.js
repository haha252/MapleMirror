(function () {
  var a = window.admin, u = window.adminNodeInspection, list = window.adminNodeList;
  var detail = window.adminNodeDetail, view = window.adminNodeWindow;
  if (!u || !list || !detail || !view) return;
  var currentNode = "", nodeCache = {}, inFlight = false, paused = false;
  var selectionVersion = 0, failures = 0, retryAt = 0, pendingRefresh = false, lastUpdated = "";
  function refreshState() {
    var state = document.getElementById("nodes-refresh-state");
    state.classList.toggle("is-paused", paused);
    state.classList.toggle("is-stale", failures > 0);
    state.textContent = paused ? "自动刷新已暂停" : failures ? "读取失败，正在重试" : "自动刷新 · 1 秒";
    a.text("nodes-updated", "最近更新 " + (lastUpdated || "—") + (failures ? " · 数据已过期" : ""));
  }
  function selectedExtras() {
    var id = currentNode, version = selectionVersion;
    if (!id) return;
    detail.load(id).then(function () {
      if (currentNode === id && selectionVersion === version && nodeCache[id]) detail.render(nodeCache[id]);
    });
  }
  function selectNode(id) {
    if (id && !nodeCache[id]) return;
    selectionVersion++;
    currentNode = id;
    view.show(id);
    list.render(id);
    if (id) { detail.render(nodeCache[id]); selectedExtras(); }
  }
  function loadNodes(force) {
    if (!force && (paused || document.hidden || Date.now() < retryAt)) return;
    if (inFlight) { pendingRefresh = pendingRefresh || !!force; return; }
    inFlight = true;
    u.read("/admin/api/nodes?inspection=1").then(function (data) {
      u.setClock(data);
      nodeCache = {};
      (data.nodes || []).forEach(function (node) { nodeCache[node.node_id] = node; });
      if (currentNode && !nodeCache[currentNode]) { currentNode = ""; selectionVersion++; view.show(""); }
      list.update(data.nodes || [], currentNode);
      if (currentNode) { detail.render(nodeCache[currentNode]); selectedExtras(); }
      failures = 0; retryAt = 0;
      lastUpdated = new Date().toLocaleTimeString("zh-CN", {hour12: false});
      refreshState();
    }).catch(function (err) {
      failures++;
      retryAt = Date.now() + Math.min(30000, Math.pow(2, failures - 1) * 1000);
      if (!Object.keys(nodeCache).length) {
        document.getElementById("nodes-list").innerHTML = '<p class="nodes-list-empty nodes-error">' + a.esc(err.message) + '</p>';
      }
      refreshState();
    }).then(function () {
      inFlight = false;
      if (pendingRefresh) { pendingRefresh = false; loadNodes(true); }
    });
  }
  function mutate(path, options) {
    var buttons = Array.from(document.querySelectorAll(".nodes-detail-actions button, .nodes-task-actions button"));
    buttons.forEach(function (button) { if (button.id !== "nodes-detail-close") button.disabled = true; });
    return a.api(path, options).then(function (data) {
      a.setStatus(data.message || "操作已完成");
      loadNodes(true);
      if (currentNode) selectedExtras();
    }).catch(function (err) { a.setStatus(err.message); }).then(function () {
      buttons.forEach(function (button) { button.disabled = false; });
    });
  }
  function nodeAction(button) {
    var id = button.getAttribute("data-node"), action = button.getAttribute("data-node-action");
    var node = nodeCache[id];
    if (!node) return;
    var name = node.public_name || id;
    var labels = {"sync-reset": "重新开始同步", disable: "禁用", enable: "启用", delete: "删除"};
    var message = action === "delete" ? "确认删除节点「" + name + "」？节点运行数据、任务、库存和授权记录会被清理。"
      : action === "sync-reset" ? "确认重新开始「" + name + "」的同步？将停止旧任务并重新拉取缺失文件，保留已验证文件和分片。"
      : "确认对「" + name + "」执行“" + labels[action] + "”？";
    a.confirmAction("节点操作", message, function () {
      var path = "/admin/api/nodes/" + encodeURIComponent(id);
      if (action !== "delete") path += "/" + action;
      mutate(path, {method: action === "delete" ? "DELETE" : "POST", body: "{}"});
    });
  }
  function taskAction(button) {
    var id = button.getAttribute("data-node"), task = button.getAttribute("data-task");
    var action = button.getAttribute("data-node-task-action");
    var name = button.closest(".nodes-task").querySelector("strong").textContent;
    a.confirmAction("同步任务", "确认" + (action === "retry" ? "重试" : "取消") + "「" + name + "」？", function () {
      mutate("/admin/api/nodes/" + encodeURIComponent(id) + "/sync-tasks/" + encodeURIComponent(task) + "/" + action,
        {method: "POST", body: "{}"});
    });
  }
  document.addEventListener("click", function (event) {
    var action = event.target.closest("[data-node-action]");
    if (action) return nodeAction(action);
    var task = event.target.closest("[data-node-task-action]");
    if (task) return taskAction(task);
    var row = event.target.closest("[data-node-row]");
    if (row) return selectNode(row.getAttribute("data-node-row"));
    var filter = event.target.closest("[data-node-filter]");
    if (filter) {
      document.getElementById("nodes-filter").value = filter.getAttribute("data-node-filter");
      list.render(currentNode);
    }
  });
  document.getElementById("nodes-detail-close").addEventListener("click", function () { selectNode(""); });
  document.getElementById("nodes-detail-backdrop").addEventListener("click", function () { selectNode(""); });
  ["nodes-filter", "nodes-region", "nodes-sort"].forEach(function (id) {
    document.getElementById(id).addEventListener("change", function () { list.render(currentNode, id === "nodes-sort"); });
  });
  document.getElementById("nodes-search").addEventListener("input", function () { list.render(currentNode); });
  document.getElementById("nodes-refresh-pause").addEventListener("click", function () {
    paused = !paused; this.textContent = paused ? "继续" : "暂停";
    this.setAttribute("aria-pressed", String(paused)); refreshState();
    if (!paused) loadNodes(true);
  });
  document.getElementById("nodes-refresh-now").addEventListener("click", function () { loadNodes(true); });
  var pairing = document.getElementById("pairing-create");
  if (pairing) pairing.addEventListener("click", function () {
    a.confirmAction("创建配对码", "确认创建一个 5 分钟有效的一次性配对码？", function () {
      a.api("/admin/api/pairing-codes", {method: "POST", body: JSON.stringify({ttl_seconds: 300})})
        .then(function (data) { a.infoDialog("配对码已创建", "过期时间：" + data.expires_at, data.pairing_code); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  });
  document.addEventListener("visibilitychange", function () { if (!document.hidden && !paused) loadNodes(true); });
  loadNodes(true);
  var timer = window.setInterval(function () { loadNodes(false); }, 1000);
  window.addEventListener("pagehide", function () { window.clearInterval(timer); });
  window.addEventListener("pageshow", function (event) {
    if (!event.persisted) return;
    timer = window.setInterval(function () { loadNodes(false); }, 1000);
    if (!paused) loadNodes(true);
  });
})();
