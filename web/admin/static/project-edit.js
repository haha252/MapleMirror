(function () {
  var a = window.admin;
  if (!a || a.page() !== "project-edit") return;
  var projects = [];
  var originalID = new URLSearchParams(location.search).get("id") || "";
  var currentProject = null;
  var dirty = false, saving = false;

  function val(p, name, fallback) {
    return p[name] != null ? p[name] : p[name.charAt(0).toLowerCase() + name.slice(1)] || fallback;
  }
  function rule(r) {
    return { Pattern: val(r, "Pattern", ""), Type: val(r, "Type", "glob"), Required: !!val(r, "Required", false) };
  }
  function selectorValue(p, pipeline, field, legacyField) {
    var config = val(pipeline, "Selectors", val(pipeline, "selectors", {})) || {};
    if (config[field] != null) return !!config[field];
    var classify = val(pipeline, "Classify", val(pipeline, "classify", {})) || {};
    var mode = String(val(classify, "Mode", "") || "").toLowerCase();
    if (mode !== "rules" && !!val(p, legacyField, false)) return true;
    if (mode === "regex") return false;
    var script = val(classify, "Script", {}) || {};
    if (val(script, "Path", "") || val(script, "Inline", "")) return true;
    return (val(classify, "Rules", []) || []).some(function (item) {
      var assign = val(item, "Assign", {}) || {};
      return String(val(assign, field === "SystemEnabled" ? "System" : "Architecture", "")).trim() !== "";
    });
  }
  function normalize(p) {
    var pipeline = val(p, "AssetPipeline", val(p, "asset_pipeline", {})) || {};
    return Object.assign({}, p, {
      ID: val(p, "ID", ""), Name: val(p, "Name", ""), Repository: val(p, "Repository", ""),
      Description: val(p, "Description", ""), HomepageURL: val(p, "HomepageURL", ""),
      IconPath: val(p, "IconPath", ""), Tags: val(p, "Tags", {}) || {},
      Enabled: !!val(p, "Enabled", true),
      RetainVersions: Number(val(p, "RetainVersions", 3)) || 3,
      IncludePrerelease: !!val(p, "IncludePrerelease", false),
      DownloadMultiplier: Number(val(p, "DownloadMultiplier", 1)) || 1,
      DefaultSelectionMode: val(p, "DefaultSelectionMode", ""),
      AssetInclude: (val(p, "AssetInclude", []) || []).map(rule),
      AssetExclude: (val(p, "AssetExclude", []) || []).map(rule),
      AssetPipeline: pipeline,
      ArchitectureSelectorEnabled: selectorValue(p, pipeline,
        "ArchitectureEnabled", "ArchitectureMatchEnabled"),
      SystemSelectorEnabled: selectorValue(p, pipeline,
        "SystemEnabled", "SystemMatchEnabled"),
      ArchitectureMatchEnabled: !!val(p, "ArchitectureMatchEnabled", false),
      ArchitectureRegex: val(p, "ArchitectureRegex", ""),
      SystemMatchEnabled: !!val(p, "SystemMatchEnabled", false),
      SystemRegex: val(p, "SystemRegex", "")
    });
  }

  function render(p) {
    document.getElementById("project-editor").innerHTML =
      section("基础信息", input("ID", "项目 ID", p.ID) + input("Name", "显示名称", p.Name) +
        input("Repository", "GitHub 仓库 owner/repo", p.Repository) +
        input("HomepageURL", "项目官网 URL", p.HomepageURL) +
        input("IconPath", "图标路径", p.IconPath) + area("Description", "项目描述", p.Description)) +
      section("运行策略", num("RetainVersions", "保留版本数", p.RetainVersions) +
        num("DownloadMultiplier", "下载倍率", p.DownloadMultiplier) + check("Enabled", "启用项目", p.Enabled) +
        check("IncludePrerelease", "包含预发布版本", p.IncludePrerelease)) +
      section("下载选择器", check("SystemSelectorEnabled", "显示系统选择器", p.SystemSelectorEnabled) +
        check("ArchitectureSelectorEnabled", "显示架构选择器", p.ArchitectureSelectorEnabled)) +
      section("架构识别", check("ArchitectureMatchEnabled", "启用架构匹配", p.ArchitectureMatchEnabled) +
        input("ArchitectureRegex", "架构提取正则", p.ArchitectureRegex)) +
      section("系统识别", check("SystemMatchEnabled", "启用系统匹配", p.SystemMatchEnabled) +
        input("SystemRegex", "系统提取正则", p.SystemRegex)) +
      ruleBlock("AssetInclude", "包含规则", p.AssetInclude) +
      ruleBlock("AssetExclude", "排除规则", p.AssetExclude);
  }

  var sectionIDs = {"基础信息": "basic", "运行策略": "policy", "下载选择器": "selectors", "架构识别": "architecture", "系统识别": "system"};
  function section(title, body) {
    return '<article id="editor-' + sectionIDs[title] + '" class="panel-card admin-panel project-edit-section"><h2>' + title +
      '</h2><div class="project-form">' + body + "</div></article>";
  }
  function input(name, label, value) {
    return '<label class="admin-field"><span>' + label + '</span><input data-field="' + name + '" value="' + a.esc(value) + '"></label>';
  }
  function num(name, label, value) {
    return '<label class="admin-field"><span>' + label + '</span><input data-field="' + name + '" type="number" min="1" value="' + a.esc(value) + '"></label>';
  }
  function area(name, label, value) {
    return '<label class="admin-field admin-field--wide"><span>' + label + '</span><textarea data-field="' + name + '" rows="4">' + a.esc(value) + '</textarea></label>';
  }
  function check(name, label, checked) {
    return '<label class="admin-check"><input data-field="' + name + '" type="checkbox"' + (checked ? " checked" : "") + "> " + label + "</label>";
  }
  function ruleBlock(name, title, rows) {
    return '<article id="editor-' + (name === "AssetInclude" ? "include" : "exclude") + '" class="panel-card admin-panel project-edit-section"><div class="admin-panel__head"><h2>' +
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
    p.AssetPipeline = JSON.parse(JSON.stringify(p.AssetPipeline || {}));
    p.AssetPipeline.Selectors = Object.assign({}, val(p.AssetPipeline, "Selectors", {}), {
      ArchitectureEnabled: !!p.ArchitectureSelectorEnabled,
      SystemEnabled: !!p.SystemSelectorEnabled
    });
    delete p.ArchitectureSelectorEnabled;
    delete p.SystemSelectorEnabled;
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

  function markDirty() { dirty = true; a.text("project-edit-summary", "有未保存修改"); }
  function setSaving(value) {
    saving = value;
    document.querySelectorAll("#project-editor input,#project-editor select,#project-editor textarea,#project-editor button,#project-save").forEach(function (el) { el.disabled = value; });
    a.text("project-save", value ? "正在保存…" : "保存项目");
  }
  function save() {
    if (saving || !currentProject) return;
    var p = readProject();
    if (!p.ID || !p.Name || !p.Repository) return a.setStatus("项目 ID、显示名称和仓库不能为空。");
    setSaving(true);
    a.api("/admin/api/projects").then(function (data) {
      var fresh = data.Projects || data.projects || [];
      var index = fresh.findIndex(function (item) { return val(item, "ID", "") === originalID; });
      if (originalID && index < 0) throw new Error("项目已被移除，请保留当前输入并核对配置。");
      if (fresh.some(function (item, i) { return val(item, "ID", "") === p.ID && i !== index; })) throw new Error("项目 ID 不得重复。");
      if (index >= 0) fresh[index] = p; else fresh.push(p);
      return a.api("/admin/api/projects", {method: "PUT", body: JSON.stringify({Projects: fresh})});
    }).then(function () { dirty = false; location.href = "/admin/projects"; })
      .catch(function (err) { a.setStatus(err.message); setSaving(false); });
  }
  window.addEventListener("beforeunload", function (event) { if (dirty) { event.preventDefault(); event.returnValue = ""; } });

  document.addEventListener("click", function (event) {
    var nav = event.target.closest(".ws-editor-nav a");
    if (nav) {
      event.preventDefault();
      document.querySelectorAll(".ws-editor-nav a").forEach(function (el) { el.setAttribute("aria-current", String(el === nav)); });
      var target = document.querySelector(nav.getAttribute("href")), box = document.getElementById("project-editor");
      box.scrollTop += target.getBoundingClientRect().top - box.getBoundingClientRect().top - 18;
      return;
    }
    var link = event.target.closest("a[href]");
    if (link && dirty && !saving && link.getAttribute("href").charAt(0) !== "#" && !event.ctrlKey && !event.metaKey) {
      event.preventDefault(); a.confirmAction("离开编辑页", "有未保存修改，确认放弃并离开？", function () { dirty = false; location.href = link.href; }); return;
    }
    var add = event.target.closest("[data-rule-add]");
    if (add) {
      markDirty();
      document.querySelector('[data-rules="' + add.getAttribute("data-rule-add") + '"]').insertAdjacentHTML("beforeend", ruleRow(add.getAttribute("data-rule-add"), {}));
    }
    if (event.target.closest("[data-rule-remove]")) {
      markDirty();
      event.target.closest(".rule-row").remove();
    }
  });
  document.addEventListener("input", function (event) { if (event.target.closest("#project-editor")) markDirty(); });
  document.addEventListener("change", function (event) { if (event.target.closest("#project-editor")) markDirty(); });
  document.getElementById("project-save").addEventListener("click", save);

  function loadProjects() {
    if (dirty) return Promise.resolve();
    return a.api("/admin/api/projects").then(function (data) {
      projects = data.Projects || data.projects || [];
      var found = projects.find(function (p) { return val(p, "ID", "") === originalID; });
      if (originalID && !found) throw new Error("项目不存在，请返回项目列表。");
      var current = normalize(found || {});
      currentProject = current;
      a.text("project-edit-summary", originalID ? "正在编辑 " + originalID : "正在新增项目");
      render(current);
    }).catch(function (err) { a.setStatus(err.message); });
  }

  loadProjects();

})();
