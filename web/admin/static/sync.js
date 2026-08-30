(function () {
  var a = window.admin;
  var ui = window.adminSyncCards;
  if (!a || !ui || a.page() !== "sync") return;
  var selectedProject = "", openTask = "", taskPage = 1, taskCache = {};

  function pager(el, page, total, size) {
    if (!el) return;
    var pages = Math.max(1, Math.ceil((total || 0) / size));
    el.innerHTML = '<button class="admin-secondary" data-task-page="prev">上一页</button>' +
      '<span>第 ' + page + ' / ' + pages + ' 页，共 ' + (total || 0) + ' 条</span>' +
      '<button class="admin-secondary" data-task-page="next">下一页</button>';
    el.querySelector('[data-task-page="prev"]').disabled = page <= 1;
    el.querySelector('[data-task-page="next"]').disabled = page >= pages;
  }

  function renderScans(scans) {
    var list = document.getElementById("scans-list");
    if (!scans || !scans.length) {
      list.innerHTML = '<div class="admin-record muted">暂无项目扫描状态</div>';
      return;
    }
    list.innerHTML = scans.map(function (scan) {
      return ui.scanCard(scan, selectedProject === scan.project_id);
    }).join("");
    if (selectedProject) renderLatest(selectedProject);
  }

  function scanCard(projectID) {
    var cards = document.querySelectorAll("[data-scan-card]");
    for (var i = 0; i < cards.length; i++) {
      if (cards[i].getAttribute("data-scan-card") === projectID) return cards[i];
    }
    return null;
  }

  function renderLatest(projectID) {
    var card = scanCard(projectID);
    if (!card) return;
    var box = card.querySelector("[data-scan-detail-box]");
    box.hidden = false;
    box.innerHTML = '<span class="muted">正在读取扫描详情...</span>';
    a.api("/admin/api/sync/scans/latest?project_id=" + encodeURIComponent(projectID))
      .then(function (data) {
        if (selectedProject !== projectID) return;
        card = scanCard(projectID);
        if (!card) return;
        box = card.querySelector("[data-scan-detail-box]");
        box.innerHTML = '<div class="admin-record__detail-grid">' +
          ui.detailItem("当前状态", a.scanStateLabel(data.state)) +
          ui.detailItem("接受文件", data.accepted_assets || 0) +
          ui.detailItem("拒绝文件", data.rejected_assets || 0) +
          ui.detailItem("开始时间", data.started_at || "暂无") +
          ui.detailItem("完成时间", data.completed_at || "进行中") +
          ui.detailItem("下次扫描", data.next_scan_at || "待安排") + '</div>' +
          '<details class="admin-tech-details"><summary>显示技术信息</summary>' +
          a.compactKv({"扫描 ID": data.scan_id || "", "项目 ID": data.project_id || projectID,
            "Request ID": data.request_id || ""}, "detail-plain") + '</details>';
      }).catch(function (err) {
        if (box) box.innerHTML = '<p class="admin-record__message admin-record__message--bad">' + a.esc(err.message) + '</p>';
      });
  }

  function toggleScan(projectID) {
    var old = selectedProject;
    selectedProject = old === projectID ? "" : projectID;
    if (old) {
      var oldCard = scanCard(old);
      if (oldCard) oldCard.querySelector("[data-scan-detail-box]").hidden = true;
    }
    if (!selectedProject) return loadScans();
    renderLatest(projectID);
    var card = scanCard(projectID), button = card && card.querySelector("[data-scan-detail]");
    if (button) { button.textContent = "收起"; button.setAttribute("aria-expanded", "true"); }
  }

  function runScan(projectID) {
    a.confirmAction("触发扫描", projectID ? "确认立即扫描这个项目？" : "确认立即扫描全部启用项目？", function () {
      a.api("/admin/api/sync/scans", {method: "POST", body: JSON.stringify({project_id: projectID || ""})})
        .then(function (data) { a.setStatus(data.message || "扫描任务已创建"); loadScans(); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  function loadScans() {
    return a.api("/admin/api/sync/scans").then(function (data) {
      renderScans(data.projects || []);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function renderTasks(tasks) {
    var list = document.getElementById("tasks-list");
    taskCache = {};
    if (!tasks.length) {
      list.innerHTML = '<div class="admin-record muted">当前节点暂无同步任务</div>';
      openTask = "";
      return;
    }
    list.innerHTML = tasks.map(function (task) {
      taskCache[task.task_id] = task;
      return ui.taskCard(task, openTask === task.task_id);
    }).join("");
    if (openTask && !taskCache[openTask]) openTask = "";
  }

  function taskCard(taskID) {
    var cards = document.querySelectorAll("[data-task-card]");
    for (var i = 0; i < cards.length; i++) {
      if (cards[i].getAttribute("data-task-card") === taskID) return cards[i];
    }
    return null;
  }

  function toggleTask(taskID) {
    var old = openTask;
    openTask = old === taskID ? "" : taskID;
    if (old) {
      var oldCard = taskCard(old);
      if (oldCard) oldCard.querySelector("[data-task-detail-box]").hidden = true;
    }
    if (!openTask) return loadTasks(document.getElementById("task-node-select").value, taskPage);
    var card = taskCard(taskID);
    if (!card) return;
    var box = card.querySelector("[data-task-detail-box]");
    box.hidden = false;
    box.innerHTML = ui.taskDetail(taskCache[taskID]);
    var button = card.querySelector("[data-task-detail]");
    if (button) { button.textContent = "收起"; button.setAttribute("aria-expanded", "true"); }
  }

  function loadNodes() {
    return a.api("/admin/api/nodes").then(function (data) {
      var select = document.getElementById("task-node-select"), current = select.value;
      select.innerHTML = '<option value="">选择节点</option>' + (data.nodes || []).map(function (node) {
        return '<option value="' + a.esc(node.node_id) + '">' + a.esc(node.public_name || node.node_id) + '</option>';
      }).join("");
      if (current && (data.nodes || []).some(function (node) { return node.node_id === current; })) select.value = current;
      else if (data.nodes && data.nodes[0]) select.value = data.nodes[0].node_id;
      if (select.value) loadTasks(select.value, 1);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function loadTasks(nodeID, page) {
    taskPage = page || taskPage;
    if (!nodeID) {
      document.getElementById("tasks-list").innerHTML = '<div class="admin-record muted">请选择节点</div>';
      document.getElementById("tasks-pager").innerHTML = "";
      return Promise.resolve();
    }
    return a.api("/admin/api/sync/tasks?node_id=" + encodeURIComponent(nodeID) +
      "&page=" + taskPage + "&page_size=20").then(function (data) {
      renderTasks(data.tasks || []);
      pager(document.getElementById("tasks-pager"), data.pagination.page, data.pagination.total, data.pagination.page_size);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function taskAction(button) {
    var taskID = button.getAttribute("data-task");
    var action = button.getAttribute("data-task-action");
    var task = taskCache[taskID] || {};
    var nodeID = document.getElementById("task-node-select").value;
    var name = task.file_name || a.taskTypeLabel(task.task_type);
    a.confirmAction("同步任务", "确认" + (action === "retry" ? "重试" : "取消") + "「" + name + "」？", function () {
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/sync-tasks/" +
        encodeURIComponent(taskID) + "/" + action, {method: "POST", body: "{}"})
        .then(function (data) { a.setStatus(data.message || "任务已更新"); loadTasks(nodeID, taskPage); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  document.addEventListener("click", function (event) {
    var scanDetail = event.target.closest("[data-scan-detail]");
    if (scanDetail) return toggleScan(scanDetail.getAttribute("data-scan-detail"));
    var scanRun = event.target.closest("[data-scan-run]");
    if (scanRun) return runScan(scanRun.getAttribute("data-scan-run"));
    var taskDetail = event.target.closest("[data-task-detail]");
    if (taskDetail) return toggleTask(taskDetail.getAttribute("data-task-detail"));
    var taskButton = event.target.closest("[data-task-action]");
    if (taskButton) return taskAction(taskButton);
    var pageButton = event.target.closest("[data-task-page]");
    if (pageButton) {
      var next = pageButton.getAttribute("data-task-page") === "next" ? taskPage + 1 : taskPage - 1;
      return loadTasks(document.getElementById("task-node-select").value, next);
    }
  });
  document.getElementById("scan-all").addEventListener("click", function () { runScan(""); });
  document.getElementById("task-node-select").addEventListener("change", function () {
    openTask = ""; loadTasks(this.value, 1);
  });
  document.getElementById("tasks-refresh").addEventListener("click", function () {
    loadTasks(document.getElementById("task-node-select").value, taskPage);
  });
  loadScans(); loadNodes();
  a.autoRefresh(loadScans, 20000);
  a.autoRefresh(function () { loadTasks(document.getElementById("task-node-select").value, taskPage); }, 15000);
})();
