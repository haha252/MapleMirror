(function () {
  var a = window.admin, w = window.adminWorkspace, ui = window.adminSyncCards;
  if (!a || !w || a.page() !== "sync") return;
  var page = 1, selected = "", rows = [], generation = 0;
  function rowKey(row) { return row.task_id; }
  function render() {
    w.list("sync-list", rows, rowKey, function (r) {
      return '<span><strong>' + a.esc(r.file_name || a.taskTypeLabel(r.task_type)) + '</strong><span class="sub">' +
        a.esc((r.project_name || r.project_id || "维护任务") + " · " + (r.node_name || r.node_id)) + '</span></span><span>' +
        a.badge(a.taskStateLabel(r.state)) + '</span><span class="ws-row-meta">' +
        a.esc("已尝试 " + (r.attempts || 0) + " 次") + '</span>';
    }, selected);
    if (selected) show();
  }
  function show() {
    var r = rows.find(function (r) { return rowKey(r) === selected; });
    if (!r) { selected = ""; return w.detail("同步详情", "", '<div class="ws-empty">在左侧选择任务</div>'); }
    var actions = "";
    var retry = ["failed", "retry_wait"].indexOf(r.state) >= 0 || r.state === "cancelled" && r.error_message === "管理员取消";
    if (retry) actions += '<button class="admin-secondary" data-task-action="retry">重试任务</button>';
    if (["pending", "sent", "running", "retry_wait", "failed"].indexOf(r.state) >= 0) actions += '<button class="admin-secondary" data-task-action="cancel">取消任务</button>';
    var body = w.title(r.file_name || a.taskTypeLabel(r.task_type), a.badge(a.taskStateLabel(r.state))) + ui.taskDetail(r);
    w.detail("同步详情", actions, body);
  }
  function load() {
    var token = generation, state = document.getElementById("sync-state").value;
    var path = "/admin/api/sync/tasks?page=" + page + "&page_size=20&node_id=" + encodeURIComponent(document.getElementById("task-node-select").value) +
      "&q=" + encodeURIComponent(document.getElementById("sync-search").value.trim()) + (state === "active" ? "&active=1" : state === "all" ? "" : "&state=" + state);
    return w.read(path).then(function (data) {
      if (token !== generation) return;
      rows = data.tasks || [];
      w.pager("tasks-pager", data.pagination, function (next) { page = next; change(true); });
      render(); w.updated("sync-updated");
    });
  }
  var refresh = w.poll(load, 1000);
  function change(clear) { generation++; if (clear) { selected = ""; w.detail("同步详情", "", '<div class="ws-empty">在左侧选择任务</div>'); } render(); refresh(); }
  document.getElementById("workspace-refresh-now").onclick = refresh;
  w.detail("同步详情", "", '<div class="ws-empty">在左侧选择任务</div>');
  w.read("/admin/api/nodes").then(function (data) {
    w.html("task-node-select", '<option value="">全部节点</option>' + (data.nodes || []).map(function (n) { return '<option value="' + a.esc(n.node_id) + '">' + a.esc(n.public_name || n.node_id) + '</option>'; }).join(""));
    var id = new URLSearchParams(location.search).get("node_id");
    if (id) { document.getElementById("task-node-select").value = id; change(true); }
  }).catch(function (err) { a.setStatus(err.message); });
  document.getElementById("task-node-select").onchange = document.getElementById("sync-state").onchange = function () { page = 1; change(true); };
  var searchTimer;
  document.getElementById("sync-search").oninput = function () { generation++; clearTimeout(searchTimer); searchTimer = setTimeout(function () { page = 1; change(true); }, 250); };
  document.addEventListener("click", function (event) {
    var row = event.target.closest("#sync-list [data-ws-key]");
    if (row) { selected = row.getAttribute("data-ws-key"); generation++; render(); w.open(); refresh(); }
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
