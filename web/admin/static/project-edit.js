(function () {
  var a = window.admin;
  if (!a || a.page() !== "project-edit") return;
  var projects = [];
  var originalID = new URLSearchParams(location.search).get("id") || "";
  var currentProject = null;

  function val(p, name, fallback) {
    return p[name] != null ? p[name] : p[name.charAt(0).toLowerCase() + name.slice(1)] || fallback;
  }
  function rule(r) {
    return { Pattern: val(r, "Pattern", ""), Type: val(r, "Type", "glob"), Required: !!val(r, "Required", false) };
  }
  function normalize(p) {
    return {
      ID: val(p, "ID", ""), Name: val(p, "Name", ""), Repository: val(p, "Repository", ""),
      IconPath: val(p, "IconPath", ""), Enabled: !!val(p, "Enabled", true),
      RetainVersions: Number(val(p, "RetainVersions", 3)) || 3,
      IncludePrerelease: !!val(p, "IncludePrerelease", false),
      DownloadMultiplier: Number(val(p, "DownloadMultiplier", 1)) || 1,
      AssetInclude: (val(p, "AssetInclude", []) || []).map(rule),
      AssetExclude: (val(p, "AssetExclude", []) || []).map(rule),
      AssetPipeline: val(p, "AssetPipeline", val(p, "asset_pipeline", {})) || {},
      ArchitectureMatchEnabled: !!val(p, "ArchitectureMatchEnabled", false),
      ArchitectureRegex: val(p, "ArchitectureRegex", ""),
      SystemMatchEnabled: !!val(p, "SystemMatchEnabled", false),
      SystemRegex: val(p, "SystemRegex", "")
    };
  }

  function render(p) {
    document.getElementById("project-editor").innerHTML =
      section("基础信息", input("ID", "项目 ID", p.ID) + input("Name", "显示名称", p.Name) +
        input("Repository", "GitHub 仓库 owner/repo", p.Repository) + input("IconPath", "图标路径", p.IconPath)) +
      section("运行策略", num("RetainVersions", "保留版本数", p.RetainVersions) +
        num("DownloadMultiplier", "下载倍率", p.DownloadMultiplier) + check("Enabled", "启用项目", p.Enabled) +
        check("IncludePrerelease", "包含预发布版本", p.IncludePrerelease)) +
      section("架构识别", check("ArchitectureMatchEnabled", "启用架构匹配", p.ArchitectureMatchEnabled) +
        input("ArchitectureRegex", "架构提取正则", p.ArchitectureRegex)) +
      section("系统识别", check("SystemMatchEnabled", "启用系统匹配", p.SystemMatchEnabled) +
        input("SystemRegex", "系统提取正则", p.SystemRegex)) +
      ruleBlock("AssetInclude", "包含规则", p.AssetInclude) +
      ruleBlock("AssetExclude", "排除规则", p.AssetExclude);
  }

  function section(title, body) {
    return '<article class="panel-card admin-panel project-edit-section"><h2>' + title +
      '</h2><div class="project-form">' + body + "</div></article>";
  }
  function input(name, label, value) {
    return '<label class="admin-field"><span>' + label + '</span><input data-field="' + name + '" value="' + a.esc(value) + '"></label>';
  }
  function num(name, label, value) {
    return '<label class="admin-field"><span>' + label + '</span><input data-field="' + name + '" type="number" min="1" value="' + a.esc(value) + '"></label>';
  }
  function check(name, label, checked) {
    return '<label class="admin-check"><input data-field="' + name + '" type="checkbox"' + (checked ? " checked" : "") + "> " + label + "</label>";
  }
  function ruleBlock(name, title, rows) {
    return '<article class="panel-card admin-panel project-edit-section"><div class="admin-panel__head"><h2>' +
      title + '</h2><button class="admin-secondary" data-rule-add="' + name + '" type="button">添加规则</button></div>' +
      '<div data-rules="' + name + '">' + (rows || []).map(function (r) { return ruleRow(name, r); }).join("") + "</div></article>";
  }
  function ruleRow(group, r) {
    return '<div class="rule-row" data-rule-row="' + group + '">' +
      '<input data-rule-field="Pattern" value="' + a.esc(r.Pattern || "") + '" placeholder="匹配内容">' +
      '<select data-rule-field="Type"><option value="glob"' + (r.Type !== "regex" ? " selected" : "") + '>通配符</option><option value="regex"' + (r.Type === "regex" ? " selected" : "") + ">正则表达式</option></select>" +
      '<label><input data-rule-field="Required" type="checkbox"' + (r.Required ? " checked" : "") + "> 必需</label>" +
      '<button class="admin-secondary" data-rule-remove type="button">删除</button></div>';
  }

  function readProject() {
    var p = normalize(currentProject || {});
    document.querySelectorAll("[data-field]").forEach(function (el) {
      var name = el.getAttribute("data-field");
      p[name] = el.type === "checkbox" ? el.checked : el.type === "number" ? Number(el.value) : el.value.trim();
    });
    p.AssetInclude = readRules("AssetInclude");
    p.AssetExclude = readRules("AssetExclude");
    return p;
  }
  function readRules(group) {
    return Array.prototype.slice.call(document.querySelectorAll('[data-rule-row="' + group + '"]')).map(function (row) {
      var out = { Pattern: "", Type: "glob", Required: false };
      row.querySelectorAll("[data-rule-field]").forEach(function (el) {
        out[el.getAttribute("data-rule-field")] = el.type === "checkbox" ? el.checked : el.value.trim();
      });
      return out;
    }).filter(function (r) { return r.Pattern; });
  }

  function save() {
    var p = readProject();
    if (!p.ID || !p.Name || !p.Repository) return a.setStatus("项目 ID、显示名称和仓库不能为空。");
    var index = projects.findIndex(function (item) { return item.ID === originalID; });
    var duplicate = projects.some(function (item, i) { return item.ID === p.ID && i !== index; });
    if (duplicate) return a.setStatus("项目 ID 不得重复。");
    if (index >= 0) projects[index] = p; else projects.push(p);
    a.api("/admin/api/projects", { method: "PUT", body: JSON.stringify({ Projects: projects }) })
      .then(function () { location.href = "/admin/projects"; })
      .catch(function (err) { a.setStatus(err.message); });
  }

  document.addEventListener("click", function (event) {
    var add = event.target.closest("[data-rule-add]");
    if (add) document.querySelector('[data-rules="' + add.getAttribute("data-rule-add") + '"]').insertAdjacentHTML("beforeend", ruleRow(add.getAttribute("data-rule-add"), {}));
    if (event.target.closest("[data-rule-remove]")) event.target.closest(".rule-row").remove();
  });
  document.getElementById("project-save").addEventListener("click", save);
  a.api("/admin/api/projects").then(function (data) {
    projects = (data.Projects || data.projects || []).map(normalize);
    var current = projects.find(function (p) { return p.ID === originalID; }) || normalize({});
    currentProject = current;
    a.text("project-edit-summary", originalID ? "正在编辑 " + originalID : "正在新增项目");
    render(current);
  }).catch(function (err) { a.setStatus(err.message); });
})();
