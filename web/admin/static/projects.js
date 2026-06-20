(function () {
  var a = window.admin;
  if (!a || a.page() !== "projects") return;
  var projects = [];

  function val(p, name, fallback) {
    return p[name] != null ? p[name] : p[name.charAt(0).toLowerCase() + name.slice(1)] || fallback;
  }

  function normalize(p) {
    return {
      ID: val(p, "ID", ""), Name: val(p, "Name", ""),
      Repository: val(p, "Repository", ""), IconPath: val(p, "IconPath", ""),
      Enabled: !!val(p, "Enabled", false),
      RetainVersions: Number(val(p, "RetainVersions", 3)) || 3,
      IncludePrerelease: !!val(p, "IncludePrerelease", false),
      DownloadMultiplier: Number(val(p, "DownloadMultiplier", 1)) || 1,
      AssetInclude: val(p, "AssetInclude", []) || [],
      AssetExclude: val(p, "AssetExclude", []) || [],
      ArchitectureMatchEnabled: !!val(p, "ArchitectureMatchEnabled", false),
      SystemMatchEnabled: !!val(p, "SystemMatchEnabled", false)
    };
  }

  function renderCards() {
    var grid = document.getElementById("project-grid");
    if (!projects.length) {
      grid.innerHTML = '<div class="panel-card admin-panel muted">暂无项目，请新增镜像项目。</div>';
    } else {
      grid.innerHTML = projects.map(function (p) {
        return '<article class="panel-card project-card admin-project-card">' +
          '<div class="project-card__meta"><img class="project-card__icon" src="/static/project-icons/' + encodeURIComponent(p.ID) + '" alt="">' +
          '<div class="project-card__title"><h2>' + a.esc(p.Name || p.ID) + '</h2><p class="project-repository">' + a.esc(p.Repository) + "</p></div></div>" +
          '<div class="project-card__summary"><p>ID<br><strong>' + a.esc(p.ID) + '</strong></p><p>状态<br>' + a.badge(p.Enabled ? "启用" : "禁用") + '</p><p>保留版本<br><strong>' + a.esc(p.RetainVersions) + "</strong></p></div>" +
          '<div class="project-card__summary"><p>预发布：' + (p.IncludePrerelease ? "包含" : "排除") + '</p><p>下载倍率：' + a.esc(p.DownloadMultiplier) + '</p></div>' +
          '<div class="project-card__summary"><p>包含规则：' + (p.AssetInclude || []).length + '</p><p>排除规则：' + (p.AssetExclude || []).length + '</p></div>' +
          '<div class="project-card__actions"><div class="admin-actions">' +
          '<a class="admin-secondary admin-link-button" href="/admin/projects/edit?id=' + encodeURIComponent(p.ID) + '">修改</a>' +
          '<button class="admin-secondary" data-project-toggle="' + a.esc(p.ID) + '">' + (p.Enabled ? "禁用" : "启用") + "</button>" +
          '<button class="admin-secondary" data-project-reset="' + a.esc(p.ID) + '">重置</button>' +
          '<button class="admin-secondary admin-danger" data-project-delete="' + a.esc(p.ID) + '">删除</button>' +
          "</div></div></article>";
      }).join("");
    }
    a.text("projects-summary", projects.length + " 个项目");
  }

  function saveAll(message) {
    return a.api("/admin/api/projects", { method: "PUT", body: JSON.stringify({ Projects: projects }) })
      .then(function (data) {
        projects = (data.projects || projects).map(normalize);
        renderCards();
        a.setStatus(message || data.message || "项目配置已保存");
      }).catch(function (err) { a.setStatus(err.message); });
  }

  function loadProjects() {
    return a.api("/admin/api/projects").then(function (data) {
      projects = (data.Projects || data.projects || []).map(normalize);
      renderCards();
    }).catch(function (err) { a.setStatus(err.message); });
  }

  function byID(id) {
    for (var i = 0; i < projects.length; i++) if (projects[i].ID === id) return i;
    return -1;
  }

  document.addEventListener("click", function (event) {
    var toggle = event.target.closest("[data-project-toggle]");
    if (toggle) {
      var ti = byID(toggle.getAttribute("data-project-toggle"));
      projects[ti].Enabled = !projects[ti].Enabled;
      return saveAll("项目启用状态已保存。");
    }
    var del = event.target.closest("[data-project-delete]");
    if (del) {
      return a.confirmAction("删除项目", "确认删除该项目定义？历史统计不会被清空。", function () {
        projects.splice(byID(del.getAttribute("data-project-delete")), 1);
        saveAll("项目定义已删除。");
      });
    }
    var reset = event.target.closest("[data-project-reset]");
    if (reset) return resetProject(reset.getAttribute("data-project-reset"));
  });

  function resetProject(id) {
    a.confirmAction("重置项目", "确认清空项目 " + id + " 的派生数据并重新扫描？", function () {
      a.api("/admin/api/projects/" + encodeURIComponent(id) + "/reset", { method: "POST", body: "{}" })
        .then(function (data) { a.setStatus(data.message || "项目已重置"); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  document.getElementById("projects-save").addEventListener("click", function () { saveAll(); });
  loadProjects();
  a.autoRefresh(loadProjects, 30000);
})();
