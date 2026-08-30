(function () {
  var a = window.admin;
  if (!a || a.page() !== "node-management") return;
  var nodeID = decodeURIComponent(location.pathname.split("/")[3] || "");
  var projects = [];

  function load() {
    Promise.all([
      a.api("/admin/api/nodes"),
      a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/projects")
    ])
      .then(function (items) {
        var node = findNode(items[0].nodes || []);
        var data = items[1];
        document.getElementById("node-download-priority").value =
          node && node.download_priority != null ? node.download_priority : 50;
        document.getElementById("node-region").value =
          node && node.region ? node.region : "unknown";
        renderProjects(data);
      }).catch(function (err) { a.setStatus(err.message); });
  }

  function findNode(nodes) {
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].node_id === nodeID) return nodes[i];
    }
    return null;
  }

  function renderProjects(data) {
    projects = data.projects || [];
    document.getElementById("node-project-auto").checked = data.assignment_mode !== "manual";
    a.text("node-project-summary", nodeID);
    a.text("node-project-limit", data.max_mirror_projects > 0
      ? "最大镜像项目数：" + data.max_mirror_projects
      : "最大镜像项目数：不限制");
    render();
  }

  function savePriority() {
    var input = document.getElementById("node-download-priority");
    var priority = Number(input.value);
    if (!Number.isInteger(priority) || priority < 0 || priority > 100) {
      a.setStatus("下载优先级必须是 0-100 的整数");
      return;
    }
    a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/priority", {
      method: "POST",
      body: JSON.stringify({ download_priority: priority })
    })
      .then(function (data) {
        a.setStatus(data.message || "节点下载优先级已更新");
        input.value = data.download_priority;
      }).catch(function (err) { a.setStatus(err.message); });
  }

  function saveRegion() {
    var region = document.getElementById("node-region").value;
    a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/region", {
      method: "POST",
      body: JSON.stringify({ region: region })
    })
      .then(function (data) {
        a.setStatus(data.message || "节点地区已更新");
        document.getElementById("node-region").value = data.region || "unknown";
      }).catch(function (err) { a.setStatus(err.message); });
  }

  function render() {
    var body = document.getElementById("node-project-body");
    var auto = document.getElementById("node-project-auto").checked;
    if (!body) return;
    if (!projects.length) {
      body.innerHTML = '<tr><td colspan="4" class="muted">暂无可分配项目</td></tr>';
      return;
    }
    body.innerHTML = projects.map(function (item) {
      return '<tr><td data-label="分配"><input type="checkbox" data-project-id="' + a.esc(item.project_id) +
        '"' + (item.assigned ? " checked" : "") + (auto ? " disabled" : "") +
        ' aria-label="分配项目"></td><td data-label="项目"><strong>' + a.esc(item.name || item.project_id) +
        '</strong><span class="sub">' + a.esc(item.project_id) + '</span></td><td data-label="30 天下载量">' +
        a.esc(item.score || 0) + '</td><td data-label="最近变化">' + a.esc(item.last_changed_at || "暂无") +
        "</td></tr>";
    }).join("");
  }

  function selectedProjects() {
    var out = [];
    document.querySelectorAll("[data-project-id]").forEach(function (box) {
      if (box.checked) out.push(box.getAttribute("data-project-id"));
    });
    return out;
  }

  document.getElementById("node-project-auto").addEventListener("change", render);
  document.getElementById("node-priority-save").addEventListener("click", savePriority);
  document.getElementById("node-region-save").addEventListener("click", saveRegion);
  document.getElementById("node-project-save").addEventListener("click", function () {
    var auto = document.getElementById("node-project-auto").checked;
    var payload = {
      assignment_mode: auto ? "auto" : "manual",
      projects: auto ? [] : selectedProjects()
    };
    a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/projects", {
      method: "PUT", body: JSON.stringify(payload)
    }).then(function (data) {
      a.setStatus(data.message || "节点项目分配已保存");
      load();
    }).catch(function (err) { a.setStatus(err.message); });
  });

  load();
  a.autoRefresh(load, 30000);
})();
