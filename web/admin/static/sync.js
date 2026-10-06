(function () {
  var a = window.admin, w = window.adminWorkspace, ui = window.adminSyncCards;
  if (!a || !w || a.page() !== "sync") return;
  var view = "tasks", page = 1, selected = "", rows = [], scans = [], latest = Object.create(null), generation = 0, configAt = 0;
  function rowKey(row) { return view === "tasks" ? row.task_id : row.project_id; }
  function render() {
    var query = document.getElementById("sync-search").value.trim().toLowerCase();
    var filtered = view === "tasks" ? rows : scans.filter(function (s) { return [s.project_name, s.project_id, s.last_scan_state, s.last_error_message].join(" ").toLowerCase().indexOf(query) >= 0; });
    w.list("sync-list", filtered, rowKey, function (r) {
      return '<span><strong>' + a.esc(view === "tasks" ? r.file_name || a.taskTypeLabel(r.task_type) : r.project_name || r.project_id) + '</strong><span class="sub">' +
        a.esc(view === "tasks" ? (r.project_name || r.project_id || "维护任务") + " · " + (r.node_name || r.node_id) : r.last_error_message || "下次扫描 " + (r.next_scan_at || "待安排")) + '</span></span><span>' +
        a.badge(view === "tasks" ? a.taskStateLabel(r.state) : a.scanStateLabel(r.last_scan_state)) + '</span><span class="ws-row-meta">' +
        a.esc(view === "tasks" ? "已尝试 " + (r.attempts || 0) + " 次" : r.enabled ? "已启用" : "已停用") + '</span>';
    }, selected);
    if (selected) show();
  }
  function show() {
    var r = (view === "tasks" ? rows : scans).find(function (r) { return rowKey(r) === selected; });
    if (!r) { selected = ""; return w.detail("同步详情", "", '<div class="ws-empty">在左侧选择任务或项目</div>'); }
    var actions = "", body;
    if (view === "tasks") {
      var retry = ["failed", "retry_wait"].indexOf(r.state) >= 0 || r.state === "cancelled" && r.error_message === "管理员取消";
      if (retry) actions += '<button class="admin-secondary" data-task-action="retry">重试任务</button>';
      if (["pending", "sent", "running", "retry_wait", "failed"].indexOf(r.state) >= 0) actions += '<button class="admin-secondary" data-task-action="cancel">取消任务</button>';
      body = w.title(r.file_name || a.taskTypeLabel(r.task_type), a.badge(a.taskStateLabel(r.state))) + ui.taskDetail(r);
    } else {
      actions = '<button class="admin-primary" data-scan-run' + (r.enabled ? "" : " disabled") + '>立即扫描</button>';
      var d = latest[r.project_id];
      body = w.title(r.project_name || r.project_id, a.badge(a.scanStateLabel(r.last_scan_state))) +
        (r.last_error_message ? '<p class="ws-note ws-note--bad">' + a.esc(r.last_error_message) + '</p>' : "") +
        w.section("扫描状态", w.kv([["下次扫描", r.next_scan_at || "待安排"], ["更新时间", r.updated_at || "—"]])) +
        (d ? w.section("最近扫描", w.kv([["状态", a.scanStateLabel(d.state)], ["接受文件", d.accepted_assets || 0], ["拒绝文件", d.rejected_assets || 0], ["开始时间", d.started_at || "—"], ["完成时间", d.completed_at || "进行中"]])) : '<p class="muted">暂无扫描详情</p>');
    }
    w.detail("同步详情", actions, body);
  }
  function load() {
    var token = generation, requests = [];
    if (Date.now() - configAt >= 30000 || view === "scans") requests.push(w.read("/admin/api/sync/scans").then(function (data) {
      scans = data.projects || []; configAt = Date.now();
      w.metrics("sync-metrics", [["扫描项目", scans.length], ["启用项目", scans.filter(function (s) { return s.enabled; }).length], ["扫描中", scans.filter(function (s) { return s.last_scan_state === "running"; }).length], ["扫描失败", scans.filter(function (s) { return s.last_scan_state === "failed"; }).length], ["待扫描", scans.filter(function (s) { return s.enabled && s.last_scan_state !== "running"; }).length]]);
    }));
    if (view === "tasks") {
      var state = document.getElementById("sync-state").value;
      var path = "/admin/api/sync/tasks?page=" + page + "&page_size=20&node_id=" + encodeURIComponent(document.getElementById("task-node-select").value) +
        "&q=" + encodeURIComponent(document.getElementById("sync-search").value.trim()) + (state === "active" ? "&active=1" : state === "all" ? "" : "&state=" + state);
      requests.push(w.read(path).then(function (data) { if (token !== generation) return; rows = data.tasks || []; w.pager("tasks-pager", data.pagination, function (next) { page = next; change(); }); }));
    } else if (selected) {
      var id = selected;
      requests.push(w.read("/admin/api/sync/scans/latest?project_id=" + encodeURIComponent(id)).then(function (data) { latest[id] = data; }, function () { delete latest[id]; }));
    }
    return Promise.all(requests).then(function () { if (token === generation) { render(); w.updated("sync-updated"); } });
  }
  var refresh = w.poll(load, 1000);
  function change(clear) { generation++; if (clear) { selected = ""; w.detail("同步详情", "", '<div class="ws-empty">在左侧选择任务或项目</div>'); } render(); refresh(); }
  document.getElementById("workspace-refresh-now").onclick = function () { configAt = 0; refresh(); };
  w.detail("同步详情", "", '<div class="ws-empty">在左侧选择任务或项目</div>');
  w.read("/admin/api/nodes").then(function (data) {
    w.html("task-node-select", '<option value="">全部节点</option>' + (data.nodes || []).map(function (n) { return '<option value="' + a.esc(n.node_id) + '">' + a.esc(n.public_name || n.node_id) + '</option>'; }).join(""));
    var id = new URLSearchParams(location.search).get("node_id");
    if (id) { document.getElementById("task-node-select").value = id; change(true); }
  }).catch(function (err) { a.setStatus(err.message); });
  document.getElementById("task-node-select").onchange = document.getElementById("sync-state").onchange = function () { page = 1; change(true); };
  var searchTimer;
  document.getElementById("sync-search").oninput = function () { generation++; clearTimeout(searchTimer); searchTimer = setTimeout(function () { page = 1; change(true); }, 250); };
  function runScan(id) {
    a.confirmAction("触发扫描", id ? "确认扫描当前项目？" : "确认扫描全部启用项目？", function () {
      a.api("/admin/api/sync/scans", {method: "POST", body: JSON.stringify({project_id: id})}).then(function (data) { a.setStatus(data.message || "扫描已创建"); configAt = 0; refresh(); }).catch(function (err) { a.setStatus(err.message); });
    });
  }
  document.getElementById("scan-all").onclick = function () { runScan(""); };
  document.addEventListener("click", function (event) {
    var tab = event.target.closest("[data-sync-view]");
    if (tab) { view = tab.getAttribute("data-sync-view"); document.querySelectorAll("[data-sync-view]").forEach(function (t) { t.setAttribute("aria-selected", String(t === tab)); }); document.getElementById("task-node-select").hidden = document.getElementById("sync-state").hidden = view === "scans"; w.html("tasks-pager", ""); change(true); }
    var row = event.target.closest("#sync-list [data-ws-key]");
    if (row) { selected = row.getAttribute("data-ws-key"); generation++; render(); w.open(); refresh(); }
    if (event.target.closest("[data-scan-run]")) runScan(selected);
    var action = event.target.closest("[data-task-action]");
    if (action) {
      var r = rows.find(function (r) { return r.task_id === selected; }); if (!r) return;
      var operation = action.getAttribute("data-task-action");
      a.confirmAction("同步任务", "确认" + (operation === "retry" ? "重试" : "取消") + "当前任务？", function () {
        a.api("/admin/api/nodes/" + encodeURIComponent(r.node_id) + "/sync-tasks/" + encodeURIComponent(r.task_id) + "/" + operation, {method: "POST", body: "{}"}).then(function (data) { a.setStatus(data.message || "任务已更新"); refresh(); }).catch(function (err) { a.setStatus(err.message); });
      });
    }
  });
})();
