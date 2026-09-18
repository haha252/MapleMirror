(function () {
  var a = window.admin;
  if (!a || a.page() !== "projects") return;
  var projects = [], developerOpen = "", moreOpen = "", revealedTokens = {};

  function val(p, name, fallback) {
    return p[name] != null ? p[name] : p[name.charAt(0).toLowerCase() + name.slice(1)] || fallback;
  }

  function normalize(p) {
    return {
      ID: val(p, "ID", ""), Name: val(p, "Name", ""), Repository: val(p, "Repository", ""),
      IconPath: val(p, "IconPath", ""), Tags: val(p, "Tags", {}) || {},
      Enabled: !!val(p, "Enabled", false),
      RetainVersions: Number(val(p, "RetainVersions", 3)) || 3,
      IncludePrerelease: !!val(p, "IncludePrerelease", false),
      DownloadMultiplier: Number(val(p, "DownloadMultiplier", 1)) || 1,
      AssetInclude: val(p, "AssetInclude", []) || [], AssetExclude: val(p, "AssetExclude", []) || [],
      ArchitectureMatchEnabled: !!val(p, "ArchitectureMatchEnabled", false),
      SystemMatchEnabled: !!val(p, "SystemMatchEnabled", false)
    };
  }

  function projectCard(p) {
    var devOpen = developerOpen === p.ID, extraOpen = moreOpen === p.ID;
    return '<article class="panel-card project-card admin-project-card">' +
      '<div class="project-card__meta"><img class="project-card__icon" src="/static/project-icons/' +
      encodeURIComponent(p.ID) + '" alt=""><div class="project-card__title"><h2>' +
      a.esc(p.Name || p.ID) + '</h2><p class="project-repository">' + a.esc(p.Repository) +
      '</p></div></div><div class="project-card__quick">' + a.badge(p.Enabled ? "已启用" : "已禁用") +
      '<span>保留 <strong>' + a.esc(p.RetainVersions) + '</strong> 个版本</span><span>' +
      (p.IncludePrerelease ? "包含预发布" : "仅稳定版") + '</span></div>' +
      '<div class="project-card__actions"><div class="admin-actions">' +
      '<a class="admin-secondary admin-link-button" href="/admin/projects/edit?id=' + encodeURIComponent(p.ID) +
      '">修改</a><button class="admin-secondary" data-project-developer="' + a.esc(p.ID) +
      '" aria-expanded="' + devOpen + '">开发者 API</button><button class="admin-secondary" data-project-more="' +
      a.esc(p.ID) + '" aria-expanded="' + extraOpen + '">' + (extraOpen ? "收起" : "更多") +
      '</button></div></div><div class="project-card__more"' + (extraOpen ? "" : " hidden") + '>' +
      '<div class="admin-record__detail-grid">' +
      detail("下载倍率", "×" + p.DownloadMultiplier) +
      detail("包含规则", (p.AssetInclude || []).length + " 条") +
      detail("排除规则", (p.AssetExclude || []).length + " 条") +
      detail("架构识别", p.ArchitectureMatchEnabled ? "启用" : "关闭") +
      detail("系统识别", p.SystemMatchEnabled ? "启用" : "关闭") + '</div>' +
      '<div class="admin-record__actions admin-record__management">' +
      '<button class="admin-secondary" data-project-toggle="' + a.esc(p.ID) + '">' +
      (p.Enabled ? "禁用项目" : "启用项目") + '</button><button class="admin-secondary" data-project-reset="' +
      a.esc(p.ID) + '">重置派生数据</button><button class="admin-secondary" data-project-version-reset="' +
      a.esc(p.ID) + '">重置版本状态</button><button class="admin-secondary admin-danger" data-project-delete="' +
      a.esc(p.ID) + '">删除项目</button></div><details class="admin-tech-details"><summary>显示技术信息</summary>' +
      a.compactKv({"项目 ID": p.ID}, "detail-plain") + '</details></div>' +
      '<div class="project-card__developer" data-developer-panel="' + a.esc(p.ID) + '"' +
      (devOpen ? "" : " hidden") + '></div></article>';
  }

  function detail(label, value) {
    return '<div class="admin-record__detail-item"><span>' + a.esc(label) +
      '</span><strong>' + a.esc(value) + '</strong></div>';
  }

  function renderCards() {
    var grid = document.getElementById("project-grid");
    grid.innerHTML = projects.length ? projects.map(projectCard).join("") :
      '<div class="panel-card admin-panel muted">暂无项目，请新增镜像项目。</div>';
    a.text("projects-summary", projects.length + " 个项目");
    if (developerOpen) loadDeveloperInfo(developerOpen);
  }

  function saveAll(message) {
    return a.api("/admin/api/projects", {method: "PUT", body: JSON.stringify({Projects: projects})})
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

  function projectName(id) {
    var index = byID(id);
    return index >= 0 ? (projects[index].Name || id) : id;
  }

  function developerPanel(id) {
    var panels = document.querySelectorAll("[data-developer-panel]");
    for (var i = 0; i < panels.length; i++) {
      if (panels[i].getAttribute("data-developer-panel") === id) return panels[i];
    }
    return null;
  }

  function loadDeveloperInfo(id) {
    var panel = developerPanel(id);
    if (!panel || panel.hidden) return;
    panel.innerHTML = '<p class="muted">正在读取 Developer API 信息...</p>';
    a.api("/admin/api/projects/" + encodeURIComponent(id) + "/developer-api")
      .then(function (info) { if (developerOpen === id) renderDeveloperInfo(id, info); })
      .catch(function (err) {
        if (developerOpen === id && panel) panel.innerHTML = '<p class="muted">' + a.esc(err.message) + '</p>';
      });
  }

  function renderDeveloperInfo(id, info) {
    var panel = developerPanel(id);
    if (!panel) return;
    var index = byID(id), project = index >= 0 ? projects[index] : null;
    var token = revealedTokens[id] || "", placeholder = token || "<YOUR_TOKEN>";
    var curl = 'curl -X POST -H "Authorization: Bearer ' + placeholder + '" "' + (info.endpoint || "") + '"';
    var state = info.token_configured ? "已启用 · " + (info.token_prefix || "") + "••••" : "尚未生成 Token";
    var disabled = project && !project.Enabled ?
      '<p class="project-developer__notice">项目当前已禁用：Token 会保留，但外部更新 API 暂停接受触发。</p>' : "";
    var reveal = token ? '<div class="project-developer__token"><span>本次生成的完整 Token（服务器不会再次返回）</span><code>' +
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
      (info.token_configured ? "重置 Token" : "生成 Token") + '</button></div>';
  }

  function rotateDeveloperToken(id, exists) {
    var run = function () {
      a.api("/admin/api/projects/" + encodeURIComponent(id) + "/developer-api/token", {method: "POST", body: "{}"})
        .then(function (data) {
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
    }).catch(function () { a.setStatus("复制失败，请手动复制"); });
  }

  function resetProject(id) {
    a.confirmAction("重置项目", "确认清空「" + projectName(id) + "」的派生数据并重新扫描？", function () {
      a.api("/admin/api/projects/" + encodeURIComponent(id) + "/reset", {method: "POST", body: "{}"})
        .then(function (data) { a.setStatus(data.message || "项目已重置"); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  function resetProjectVersions(id) {
    a.confirmAction("重置版本状态", "将放弃「" + projectName(id) +
      "」当前的版本选择基线，并以现在的上游 Release 列表重新扫描。适用于开发者撤回或删除 Release 后手动恢复。下载次数、流量和历史统计不会被清除。确认继续？", function () {
      a.api("/admin/api/projects/" + encodeURIComponent(id) + "/reset-versions", {method: "POST", body: "{}"})
        .then(function (data) { a.setStatus(data.message || "版本状态重置扫描任务已创建"); })
        .catch(function (err) { a.setStatus(err.message); });
    });
  }

  document.addEventListener("click", function (event) {
    var developer = event.target.closest("[data-project-developer]");
    if (developer) {
      var id = developer.getAttribute("data-project-developer");
      developerOpen = developerOpen === id ? "" : id;
      return renderCards();
    }
    var more = event.target.closest("[data-project-more]");
    if (more) {
      var moreID = more.getAttribute("data-project-more");
      moreOpen = moreOpen === moreID ? "" : moreID;
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
      return saveAll("项目启用状态已保存");
    }
    var reset = event.target.closest("[data-project-reset]");
    if (reset) return resetProject(reset.getAttribute("data-project-reset"));
    var versionReset = event.target.closest("[data-project-version-reset]");
    if (versionReset) return resetProjectVersions(versionReset.getAttribute("data-project-version-reset"));
    var del = event.target.closest("[data-project-delete]");
    if (del) {
      var deleteID = del.getAttribute("data-project-delete");
      return a.confirmAction("删除项目", "确认删除「" + projectName(deleteID) +
        "」？历史统计不会清空，Developer API Token 将被吊销。", function () {
        projects.splice(byID(deleteID), 1);
        if (developerOpen === deleteID) developerOpen = "";
        if (moreOpen === deleteID) moreOpen = "";
        delete revealedTokens[deleteID];
        saveAll("项目定义已删除");
      });
    }
  });

  document.getElementById("projects-save").addEventListener("click", function () { saveAll(); });
  loadProjects();
  a.autoRefresh(loadProjects, 30000);
})();
