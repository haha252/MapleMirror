(function () {
  var a = window.admin;
  if (!a || a.page() !== "sync") return;
  var selectedProject = "";
  var taskPage = 1;

  function renderPager(el, page, total, size) {
    if (!el) return;
    var pages = Math.max(1, Math.ceil((total || 0) / size));
    el.innerHTML = '<button class="admin-secondary" type="button" data-task-page="prev">上一页</button>' +
      '<span>第 ' + page + ' / ' + pages + ' 页，共 ' + (total || 0) + ' 条</span>' +
      '<button class="admin-secondary" type="button" data-task-page="next">下一页</button>';
    el.querySelector('[data-task-page="prev"]').disabled = page <= 1;
    el.querySelector('[data-task-page="next"]').disabled = page >= pages;
  }

  function renderScans(scans) {
    var body = document.getElementById("scans-body");
    if (!body) return;
    if (!scans || !scans.length) {
      body.innerHTML = '<tr><td colspan="5" class="muted">暂无项目扫描状态</td></tr>';
      return;
    }
    body.innerHTML = scans.map(function (scan) {
      return "<tr><td><strong>" + a.esc(scan.project_id) + "</strong></td><td>" +
        a.badge(scan.enabled ? "启用" : "禁用") + "</td><td>" +
        a.badge(scan.last_scan_state || "未扫描") + "</td><td>" +
        a.esc(scan.next_scan_at || "暂无") + '</td><td><div class="admin-actions">' +
        '<button class="admin-secondary" data-scan-detail="' + a.esc(scan.project_id) + '">详情</button>' +
        '<button class="admin-secondary" data-scan-run="' + a.esc(scan.project_id) + '">扫描</button></div></td></tr>';
    }).join("");
    if (!selectedProject && scans[0]) showLatest(scans[0].project_id);
  }

  function showLatest(projectID) {
    selectedProject = projectID || "";
    a.text("latest-scan-title", projectID || "全部项目");
    var box = document.getElementById("latest-scan");
    if (box) box.innerHTML = '<div class="muted">加载中...</div>';
    var path = "/admin/api/sync/scans/latest";
    if (projectID) path += "?project_id=" + encodeURIComponent(projectID);
    a.api(path).then(function (data) {
      box.innerHTML = a.kv({
        "扫描 ID": data.scan_id || "",
        "项目": data.project_id || "全部",
        "状态": data.state || "",
        "选中 Release": data.selected_releases || 0,
        "接受资产": data.accepted_assets || 0,
        "拒绝资产": data.rejected_assets || 0,
        "开始时间": data.started_at || "",
        "完成时间": data.completed_at || "",
        "错误摘要": data.last_error_message || ""
      });
    }).catch(function (err) {
      if (box) box.innerHTML = '<div class="muted">' + a.esc(err.message) + "</div>";
    });
  }

  function runScan(projectID) {
    var text = projectID ? "确认触发项目 " + projectID + " 的扫描？" : "确认触发所有启用项目扫描？";
    a.confirmAction("触发扫描", text, function () {
      a.api("/admin/api/sync/scans", {
        method: "POST",
        body: JSON.stringify({ project_id: projectID || "" })
      }).then(function (data) {
        a.setStatus(data.message || "扫描已创建");
        loadScans().then(function () { showLatest(projectID); });
      }).catch(function (err) { a.setStatus(err.message); });
    });
  }

  function loadScans() {
    return a.api("/admin/api/sync/scans").then(function (data) {
      renderScans(data.projects || []);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function loadNodes() {
    return a.api("/admin/api/nodes").then(function (data) {
      var select = document.getElementById("task-node-select");
      if (!select) return;
      select.innerHTML = '<option value="">选择节点</option>' + (data.nodes || []).map(function (node) {
        return '<option value="' + a.esc(node.node_id) + '">' +
          a.esc(node.public_name || node.node_id) + "</option>";
      }).join("");
      if (data.nodes && data.nodes[0]) {
        select.value = data.nodes[0].node_id;
        loadTasks(select.value, 1);
      }
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function loadTasks(nodeID, page) {
    var body = document.getElementById("tasks-body");
    var pager = document.getElementById("tasks-pager");
    taskPage = page || taskPage;
    if (!nodeID) {
      body.innerHTML = '<tr><td colspan="6" class="muted">请选择节点</td></tr>';
      if (pager) pager.innerHTML = "";
      return;
    }
    return a.api("/admin/api/sync/tasks?node_id=" + encodeURIComponent(nodeID) +
      "&page=" + taskPage + "&page_size=20").then(function (data) {
      var rows = data.tasks || [];
      if (!rows.length) {
        body.innerHTML = '<tr><td colspan="6" class="muted">暂无同步任务</td></tr>';
      } else {
        body.innerHTML = rows.map(function (task) {
          var canOperate = task.state === "failed" || task.state === "retry_wait" || task.state === "pending";
          return "<tr><td><strong>" + a.esc(task.task_id) + '</strong><span class="sub">' +
            a.esc(task.task_type) + "</span></td><td>" + a.esc(task.asset_id || "") +
            "</td><td>" + a.badge(task.state) + "</td><td>" +
            a.esc(task.attempts || 0) + "</td><td>" + a.esc(task.error_message || "") +
            '</td><td><div class="admin-actions">' + (canOperate ?
              '<button class="admin-secondary" data-task-action="retry" data-node="' + a.esc(task.node_id) + '" data-task="' + a.esc(task.task_id) + '">重试</button>' +
              '<button class="admin-secondary" data-task-action="cancel" data-node="' + a.esc(task.node_id) + '" data-task="' + a.esc(task.task_id) + '">取消</button>' :
              '<span class="muted">无操作</span>') + "</div></td></tr>";
        }).join("");
      }
      renderPager(pager, data.pagination.page, data.pagination.total, data.pagination.page_size);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  document.addEventListener("click", function (event) {
    var detail = event.target.closest("[data-scan-detail]");
    if (detail) return showLatest(detail.getAttribute("data-scan-detail"));
    var run = event.target.closest("[data-scan-run]");
    if (run) return runScan(run.getAttribute("data-scan-run"));
    var task = event.target.closest("[data-task-action]");
    if (task) return taskAction(task);
    var pageButton = event.target.closest("[data-task-page]");
    if (pageButton) {
      var nodeID = document.getElementById("task-node-select").value;
      var next = pageButton.getAttribute("data-task-page") === "next" ? taskPage + 1 : taskPage - 1;
      return loadTasks(nodeID, next);
    }
  });

  function taskAction(button) {
    var node = button.getAttribute("data-node");
    var task = button.getAttribute("data-task");
    var action = button.getAttribute("data-task-action");
    a.confirmAction("同步任务", "确认" + (action === "retry" ? "重试" : "取消") + "任务 " + task + "？", function () {
      a.api("/admin/api/nodes/" + encodeURIComponent(node) + "/sync-tasks/" +
        encodeURIComponent(task) + "/" + action, { method: "POST", body: "{}" })
        .then(function (data) { a.setStatus(data.message || "任务已更新"); loadTasks(node, taskPage); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  document.getElementById("scan-all").addEventListener("click", function () { runScan(""); });
  document.getElementById("task-node-select").addEventListener("change", function () { loadTasks(this.value, 1); });
  loadScans();
  loadNodes();
})();
