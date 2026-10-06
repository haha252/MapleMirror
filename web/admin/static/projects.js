(function () {
  var a = window.admin;
  if (!a || a.page() !== "projects") return;
  var projects = [];

  function val(p, name, fallback) {
    return p[name] != null ? p[name] : p[name.charAt(0).toLowerCase() + name.slice(1)] || fallback;
  }

  function normalize(p) {
    return Object.assign({}, p, {
      ID: val(p, "ID", ""), Name: val(p, "Name", ""), Repository: val(p, "Repository", ""),
      IconPath: val(p, "IconPath", ""), Tags: val(p, "Tags", {}) || {},
      Enabled: !!val(p, "Enabled", false),
      RetainVersions: Number(val(p, "RetainVersions", 3)) || 3,
      IncludePrerelease: !!val(p, "IncludePrerelease", false),
      DownloadMultiplier: Number(val(p, "DownloadMultiplier", 1)) || 1,
      AssetInclude: val(p, "AssetInclude", []) || [], AssetExclude: val(p, "AssetExclude", []) || [],
      ArchitectureMatchEnabled: !!val(p, "ArchitectureMatchEnabled", false),
      SystemMatchEnabled: !!val(p, "SystemMatchEnabled", false)
    });
  }

  var w = window.adminWorkspace, selected = "", scans = Object.create(null), configAt = 0, saving = false;
  function rules(items) {
    return items.length ? items.map(function (r) { return '<p class="ws-note"><code>' + a.esc(val(r, "Pattern", "")) + '</code> · ' + a.esc(val(r, "Type", "glob")) + (val(r, "Required", false) ? ' · 必需' : '') + '</p>'; }).join("") : '<p class="muted">未配置规则</p>';
  }
  function renderCards() {
    var query = document.getElementById("projects-search").value.toLowerCase(), filter = document.getElementById("projects-filter").value;
    var filtered = projects.filter(function (p) {
      return [p.Name, p.ID, p.Repository, JSON.stringify(p.Tags)].join(" ").toLowerCase().indexOf(query) >= 0 &&
        (filter === "all" || filter === "enabled" && p.Enabled || filter === "disabled" && !p.Enabled || filter === "failed" && (scans[p.ID] || {}).last_scan_state === "failed");
    });
    w.metrics("projects-metrics", [["项目总数", projects.length], ["已启用", projects.filter(function (p) { return p.Enabled; }).length],
      ["已停用", projects.filter(function (p) { return !p.Enabled; }).length], ["扫描中", Object.keys(scans).filter(function (id) { return scans[id].last_scan_state === "running"; }).length],
      ["扫描失败", Object.keys(scans).filter(function (id) { return scans[id].last_scan_state === "failed"; }).length]]);
    w.list("project-grid", filtered, function (p) { return p.ID; }, function (p) {
      return '<span class="ws-identity"><img class="ws-icon" src="/static/project-icons/' + encodeURIComponent(p.ID) + '" alt=""><span><strong>' + a.esc(p.Name || p.ID) + '</strong><span class="sub">' + a.esc(p.Repository) + '</span></span></span><span>' + a.badge(p.Enabled ? "已启用" : "已停用") + '</span><span class="ws-row-meta">' + a.esc(a.scanStateLabel((scans[p.ID] || {}).last_scan_state)) + '</span>';
    }, selected);
    a.text("projects-summary", filtered.length + " / " + projects.length + " 个项目");
    var index = byID(selected);
    if (index < 0) { selected = ""; return w.detail("项目详情", "", '<div class="ws-empty">在左侧选择项目</div>'); }
    var p = projects[index], scan = scans[p.ID] || {}, id = a.esc(p.ID);
    var actions = '<a class="admin-secondary admin-link-button" href="/admin/projects/edit?id=' + encodeURIComponent(p.ID) + '">编辑项目</a><button class="admin-secondary" data-project-toggle="' + id + '">' + (p.Enabled ? "停用" : "启用") + '</button><button class="admin-primary" data-project-scan="' + id + '"' + (p.Enabled ? '' : ' disabled') + '>立即扫描</button>';
    var body = w.title(p.Name || p.ID, a.badge(p.Enabled ? "已启用" : "已停用") + a.esc(" · " + p.Repository)) +
      w.section("运行与同步", w.kv([["扫描状态", a.scanStateLabel(scan.last_scan_state)], ["下次扫描", scan.next_scan_at || "待安排"], ["更新时间", scan.updated_at || "—"]])) +
      (scan.last_error_message ? '<p class="ws-note ws-note--bad">' + a.esc(scan.last_error_message) + '</p>' : '') +
      w.section("版本与下载策略", w.kv([["保留版本", p.RetainVersions], ["预发布", p.IncludePrerelease ? "包含" : "排除"], ["下载倍率", "×" + p.DownloadMultiplier], ["包含规则", p.AssetInclude.length + " 条"], ["排除规则", p.AssetExclude.length + " 条"], ["项目 ID", p.ID]])) +
      w.section("资源筛选规则", w.kv([["架构识别", p.ArchitectureMatchEnabled ? "启用" : "关闭"], ["系统识别", p.SystemMatchEnabled ? "启用" : "关闭"]]) + '<details><summary>包含规则</summary>' + rules(p.AssetInclude) + '</details><details><summary>排除规则</summary>' + rules(p.AssetExclude) + '</details>') +
      '<details class="ws-section" id="project-developer"><summary>开发者 API</summary><div data-developer-panel="' + id + '"></div></details>' +
      '<details class="ws-section"><summary>维护操作</summary><div class="ws-actions"><button class="admin-secondary" data-project-reset="' + id + '">重置派生数据</button><button class="admin-secondary" data-project-version-reset="' + id + '">重置版本状态</button><button class="admin-secondary admin-danger" data-project-delete="' + id + '">删除项目</button></div></details>';
    w.detail("项目详情", actions, body);
    if (document.getElementById("project-developer").open && !developerPanel(p.ID).innerHTML) { loadDeveloperInfo(p.ID); }
  }
  function saveAll(message, id, remove) {
    if (saving) return; saving = true;
    var desired = projects.find(function (p) { return p.ID === id; });
    return a.api("/admin/api/projects").then(function (data) {
      var fresh = data.Projects || data.projects || [], index = fresh.findIndex(function (p) { return val(p, "ID", "") === id; });
      if (index < 0) throw new Error("项目已被移除，请刷新后重试");
      if (remove) fresh.splice(index, 1); else fresh[index].Enabled = desired.Enabled;
      return a.api("/admin/api/projects", {method: "PUT", body: JSON.stringify({Projects: fresh})});
    }).then(function (data) { configAt = 0; a.setStatus(message || data.message || "已保存"); return loadProjects(); })
      .catch(function (err) { a.setStatus(err.message); configAt = 0; loadProjects().catch(function () {}); })
      .then(function () { saving = false; });
  }
  function loadProjects() {
    var requests = [w.read("/admin/api/sync/scans").then(function (data) { scans = Object.create(null); (data.projects || []).forEach(function (s) { scans[s.project_id] = s; }); })];
    if (Date.now() - configAt >= 30000) requests.push(w.read("/admin/api/projects").then(function (data) { projects = (data.Projects || data.projects || []).map(normalize); configAt = Date.now(); }));
    return Promise.all(requests).then(function () { renderCards(); w.updated("projects-updated"); });
  }

  function byID(id) {
    for (var i = 0; i < projects.length; i++) if (projects[i].ID === id) return i;
    return -1;
  }

  function projectName(id) {
    var index = byID(id);
    return index >= 0 ? (projects[index].Name || id) : id;
  }

  var developer = window.adminProjectDeveloper(function (id) { return projects[byID(id)]; }, function (id) { return selected === id; });
  var developerPanel = developer.panel, loadDeveloperInfo = developer.load, rotateDeveloperToken = developer.rotate, copyValue = developer.copy;

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
    var row = event.target.closest("#project-grid [data-ws-key]");
    if (row) { selected = row.getAttribute("data-ws-key"); renderCards(); w.open(); }
    var scan = event.target.closest("[data-project-scan]");
    if (scan) return a.confirmAction("立即扫描", "确认扫描当前项目？", function () {
      a.api("/admin/api/sync/scans", {method: "POST", body: JSON.stringify({project_id: scan.getAttribute("data-project-scan")})}).then(function (data) { a.setStatus(data.message || "扫描已创建"); refresh(); }).catch(function (err) { a.setStatus(err.message); });
    });
    var token = event.target.closest("[data-developer-token]");
    if (token) return rotateDeveloperToken(token.getAttribute("data-developer-token"),
      token.getAttribute("data-token-exists") === "true");
    var copy = event.target.closest("[data-copy-value]");
    if (copy) return copyValue(copy.getAttribute("data-copy-value"));
    var toggle = event.target.closest("[data-project-toggle]");
    if (toggle) {
      if (saving) return;
      var ti = byID(toggle.getAttribute("data-project-toggle"));
      projects[ti].Enabled = !projects[ti].Enabled;
      return saveAll("项目启用状态已保存", projects[ti].ID);
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
        if (saving) return;
        saveAll("项目定义已删除", deleteID, true);
      });
    }
  });

  document.getElementById("workspace-detail-body").addEventListener("toggle", function (event) {
    if (event.target.id === "project-developer" && event.target.open) { loadDeveloperInfo(selected); }
  }, true);
  var indexNow = document.getElementById("indexnow-submit");
  indexNow.onclick = function () {
    a.confirmAction("立即提交 IndexNow", "确认立即提交全量公开 URL？", function () {
      indexNow.disabled = true;
      a.api("/admin/api/indexnow/submit", {method: "POST", body: "{}"}).then(function (data) {
        a.setStatus((data.message || "已加入提交队列") + "（新增 " + (data.url_count || 0) + " 条）");
      }).catch(function (err) { a.setStatus(err.message); }).then(function () { indexNow.disabled = false; });
    });
  };
  document.getElementById("projects-search").oninput = renderCards;
  document.getElementById("projects-filter").onchange = renderCards;
  var refresh = w.poll(loadProjects, 1000);
  document.getElementById("workspace-refresh-now").onclick = function () { configAt = 0; refresh(); };
})();
