(function () {
  var a = window.admin;
  if (!a || a.page() !== "node-projects") return;
  var nodeID = decodeURIComponent(location.pathname.split("/")[3] || "");
  var projects = [];

  function load() {
    a.api("/admin/api/nodes/" + encodeURIComponent(nodeID) + "/projects")
      .then(function (data) {
        projects = data.projects || [];
        document.getElementById("node-project-auto").checked = data.assignment_mode !== "manual";
        a.text("node-project-summary", nodeID);
        a.text("node-project-limit", data.max_mirror_projects > 0
          ? "最大镜像项目数：" + data.max_mirror_projects
          : "最大镜像项目数：不限制");
        render();
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
      return '<tr><td><input type="checkbox" data-project-id="' + a.esc(item.project_id) +
        '"' + (item.assigned ? " checked" : "") + (auto ? " disabled" : "") +
        ' aria-label="分配项目"></td><td><strong>' + a.esc(item.name || item.project_id) +
        '</strong><span class="sub">' + a.esc(item.project_id) + '</span></td><td>' +
        a.esc(item.score || 0) + '</td><td>' + a.esc(item.last_changed_at || "暂无") +
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
})();
