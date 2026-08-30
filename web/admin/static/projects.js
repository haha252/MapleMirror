(function () {
  var a = window.admin;
  if (!a || a.page() !== "projects") return;
  var projects = [];
  var developerOpen = "";
  var revealedTokens = {};

  function val(p, name, fallback) {
    return p[name] != null ? p[name] : p[name.charAt(0).toLowerCase() + name.slice(1)] || fallback;
  }

  function normalize(p) {
    return {
      ID: val(p, "ID", ""), Name: val(p, "Name", ""),
      Repository: val(p, "Repository", ""), IconPath: val(p, "IconPath", ""),
      Tags: val(p, "Tags", {}) || {},
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
        var open = developerOpen === p.ID;
        return '<article class="panel-card project-card admin-project-card">' +
          '<div class="project-card__meta"><img class="project-card__icon" src="/static/project-icons/' + encodeURIComponent(p.ID) + '" alt="">' +
          '<div class="project-card__title"><h2>' + a.esc(p.Name || p.ID) + '</h2><p class="project-repository">' + a.esc(p.Repository) + "</p></div></div>" +
          '<div class="project-card__summary"><p>ID<br><strong>' + a.esc(p.ID) + '</strong></p><p>状态<br>' + a.badge(p.Enabled ? "启用" : "禁用") + '</p><p>保留版本<br><strong>' + a.esc(p.RetainVersions) + "</strong></p></div>" +
          '<div class="project-card__summary"><p>预发布：' + (p.IncludePrerelease ? "包含" : "排除") + '</p><p>下载倍率：' + a.esc(p.DownloadMultiplier) + '</p></div>' +
          '<div class="project-card__summary"><p>包含规则：' + (p.AssetInclude || []).length + '</p><p>排除规则：' + (p.AssetExclude || []).length + '</p></div>' +
          '<div class="project-card__actions"><div class="admin-actions">' +
          '<a class="admin-secondary admin-link-button" href="/admin/projects/edit?id=' + encodeURIComponent(p.ID) + '">修改</a>' +
          '<button class="admin-secondary" data-project-developer="' + a.esc(p.ID) + '" aria-expanded="' + open + '">开发者 API</button>' +
          '<button class="admin-secondary" data-project-toggle="' + a.esc(p.ID) + '">' + (p.Enabled ? "禁用" : "启用") + "</button>" +
          '<button class="admin-secondary" data-project-reset="' + a.esc(p.ID) + '">重置</button>' +
          '<button class="admin-secondary admin-danger" data-project-delete="' + a.esc(p.ID) + '">删除</button>' +
          '</div></div><div class="project-card__developer" data-developer-panel="' + a.esc(p.ID) + '"' +
          (open ? "" : " hidden") + "></div></article>";
      }).join("");
    }
    a.text("projects-summary", projects.length + " 个项目");
    if (developerOpen) loadDeveloperInfo(developerOpen);
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

  function developerPanel(id) {
    return document.querySelector('[data-developer-panel="' + cssEscape(id) + '"]');
  }

  function cssEscape(value) {
    if (window.CSS && CSS.escape) return CSS.escape(value);
    return String(value).replace(/["\\]/g, "\\$&");
  }

  function loadDeveloperInfo(id) {
    var panel = developerPanel(id);
    if (!panel || panel.hidden) return;
    panel.innerHTML = '<p class="muted">正在读取 Developer API 信息...</p>';
    a.api("/admin/api/projects/" + encodeURIComponent(id) + "/developer-api")
      .then(function (info) {
        if (developerOpen === id) renderDeveloperInfo(id, info);
      }).catch(function (err) {
        if (developerOpen === id && panel) panel.innerHTML = '<p class="muted">' + a.esc(err.message) + "</p>";
      });
  }

  function renderDeveloperInfo(id, info) {
    var panel = developerPanel(id);
    if (!panel) return;
    var index = byID(id);
    var project = index >= 0 ? projects[index] : null;
    var token = revealedTokens[id] || "";
    var placeholder = token || "<YOUR_TOKEN>";
    var curl = 'curl -X POST -H "Authorization: Bearer ' + placeholder + '" "' + (info.endpoint || "") + '"';
    var state = info.token_configured ? "已启用 · " + (info.token_prefix || "") + "••••" : "尚未生成 Token";
    var disabled = project && !project.Enabled ?
      '<p class="project-developer__notice">项目当前已禁用：Token 会保留，但外部更新 API 暂停接受触发。</p>' : "";
    var reveal = token ?
      '<div class="project-developer__token"><span>本次生成的完整 Token（服务器不会再次返回）</span><code>' +
      a.esc(token) + '</code><button class="admin-secondary" data-copy-value="' + a.esc(token) + '">复制 Token</button></div>' : "";
    panel.innerHTML = '<div class="project-developer__head"><strong>Developer Sync API</strong><span>' +
      a.esc(state) + '</span></div><div class="project-developer__endpoint"><span>POST</span><code>' +
      a.esc(info.endpoint || "") + '</code></div><div class="project-developer__usage"><span>今日使用 <strong>' +
      a.esc(info.used_today || 0) + " / " + a.esc(info.daily_limit || 100) + '</strong></span><span>剩余 <strong>' +
      a.esc(info.remaining_today == null ? 100 : info.remaining_today) + '</strong></span><span>最近调用 <strong>' +
      a.esc(info.last_used_at || "暂无") + '</strong></span></div>' + disabled + reveal +
      '<div class="admin-actions project-developer__actions"><button class="admin-secondary" data-copy-value="' +
      a.esc(info.endpoint || "") + '">复制 API</button><button class="admin-secondary" data-copy-value="' +
      a.esc(curl) + '">复制调用示例</button><button class="admin-secondary ' +
      (info.token_configured ? "admin-danger" : "admin-primary") + '" data-developer-token="' + a.esc(id) +
      '" data-token-exists="' + (!!info.token_configured) + '">' +
      (info.token_configured ? "重置 Token" : "生成 Token") + "</button></div>";
  }

  function rotateDeveloperToken(id, exists) {
    var run = function () {
      a.api("/admin/api/projects/" + encodeURIComponent(id) + "/developer-api/token",
        { method: "POST", body: "{}" }).then(function (data) {
          revealedTokens[id] = data.token || "";
          renderDeveloperInfo(id, data.developer_api || {});
          a.setStatus("Developer API Token 已生成，请立即复制并妥善保存");
        }).catch(function (err) { a.setStatus(err.message); });
    };
    if (!exists) return run();
    a.confirmAction("重置 Developer API Token",
      "旧 Token 将立即失效，使用旧 Token 的 GitHub Actions 或 CI 必须同步更新。确认重置？", run);
  }

  function copyValue(value) {
    navigator.clipboard.writeText(value || "").then(function () {
      a.setStatus("已复制到剪贴板");
    }).catch(function () {
      a.setStatus("复制失败，请手动复制");
    });
  }

  document.addEventListener("click", function (event) {
    var developer = event.target.closest("[data-project-developer]");
    if (developer) {
      var id = developer.getAttribute("data-project-developer");
      developerOpen = developerOpen === id ? "" : id;
      return renderCards();
    }
    var token = event.target.closest("[data-developer-token]");
    if (token) return rotateDeveloperToken(token.getAttribute("data-developer-token"),
      token.getAttribute("data-token-exists") === "true");
    var copy = event.target.closest("[data-copy-value]");
    if (copy) return copyValue(copy.getAttribute("data-copy-value"));
    var toggle = event.target.closest("[data-project-toggle]");
    if (toggle) {
      var ti = byID(toggle.getAttribute("data-project-toggle"));
      projects[ti].Enabled = !projects[ti].Enabled;
      return saveAll("项目启用状态已保存。");
    }
    var del = event.target.closest("[data-project-delete]");
    if (del) {
      return a.confirmAction("删除项目", "确认删除该项目定义？历史统计不会被清空，Developer API Token 将被吊销。", function () {
        var id = del.getAttribute("data-project-delete");
        projects.splice(byID(id), 1);
        if (developerOpen === id) developerOpen = "";
        delete revealedTokens[id];
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
