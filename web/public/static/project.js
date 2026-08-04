(function () {
  const page = document.querySelector(".project-page[data-project-id]");
  if (!page) return;
  const i18n = window.MirrorI18n;
  const text = (key, fallback, params) => i18n ? i18n.t(key, params) : fallback;
  const projectId = page.dataset.projectId || "";
  const statusBox = document.getElementById("project-download-status");
  const statusSummary = statusBox.parentElement;
  const availability = document.getElementById("project-availability");
  const versionField = document.getElementById("project-version-field");
  const versionSelect = document.getElementById("project-version");
  const systemField = document.getElementById("project-system-field");
  const systemSelect = document.getElementById("project-system");
  const archField = document.getElementById("project-architecture-field");
  const archSelect = document.getElementById("project-architecture");
  const fileBrowser = document.getElementById("project-file-browser");
  const assetSummary = page.querySelector(".project-download__asset");
  const fileName = document.getElementById("project-file-name");
  const fileMeta = document.getElementById("project-file-meta");
  const button = document.getElementById("project-download-button");
  const icon = page.querySelector(".project-hero__icon");
  const updated = page.querySelector(".project-updated");
  const modeButtons = Array.from(page.querySelectorAll("[data-selection-mode]"));
  const selectors = window.DownloadSelectors || {
    bytesText: (value) => String(value || 0) + " B",
    preferredAsset: (items) => items.find((item) => item.available) || items[0] || null,
    preferredAssetForUser: (items) => items.find((item) => item.available) || items[0] || null,
    rememberSelectionMode: () => {},
    selectionModeForProject: (_, mode) => mode === "file" ? "file" : "selectors",
    setModeButtons: () => {},
    uniqueVersions: (items) => Array.from(new Set(items.map((item) => item.version))),
    userSystem: () => ""
  };
  let project = null;
  let systemEnabled = false;
  let architectureEnabled = false;
  let mode = "selectors";
  let selectedAsset = null;
  let browser = null;
  let versions = [];
  let lastError = null;

  function selectorEnabled(item, field, legacyField) {
    if (item[field] != null) return !!item[field];
    return !!item[legacyField];
  }

  function setStatus(message, level) {
    statusBox.textContent = message || "";
    statusBox.className = level || "muted";
    statusSummary.hidden = !message;
  }

  function architectureLabel(item) {
    const raw = String(item.architecture || "").trim();
    return raw || text("system.unidentified", "未识别");
  }

  function refreshSystems(preferred) {
    if (!systemEnabled) {
      refreshArchitectures(preferred);
      return;
    }
    const list = project.assets.filter((item) => item.version === versionSelect.value);
    const systems = Array.from(new Set(list.map((item) => item.system).filter(Boolean)));
    const wanted = selectors.userSystem ? selectors.userSystem() : "";
    const preferredSystem = preferred && list.includes(preferred) ? preferred.system : "";
    const choice = systems.includes(preferredSystem) ? preferredSystem :
      systems.includes(wanted) ? wanted : systems[0] || "";
    systemSelect.innerHTML = "";
    systems.forEach((system) => {
      const option = document.createElement("option");
      option.value = system;
      option.textContent = selectors.systemLabel ? selectors.systemLabel(system) : system;
      if (system === choice) option.selected = true;
      systemSelect.appendChild(option);
    });
    refreshArchitectures(preferred);
  }

  function refreshArchitectures(preferred) {
    let list = project.assets.filter((item) => item.version === versionSelect.value);
    if (systemEnabled) {
      list = list.filter((item) => item.system === systemSelect.value);
    }
    const choice = list.includes(preferred) ? preferred :
      selectors.preferredAssetForUser(list, architectureEnabled);
    archSelect.innerHTML = "";
    if (!architectureEnabled) {
      refreshDetails(choice);
      return;
    }
    list.forEach((item) => {
      const option = document.createElement("option");
      option.value = item.asset_id;
      option.textContent = architectureLabel(item);
      if (choice && choice.asset_id === item.asset_id) option.selected = true;
      archSelect.appendChild(option);
    });
    refreshDetails(choice);
  }

  function refreshDetails(preferred) {
    const selected = preferred ||
      project.assets.find((item) => item.asset_id === archSelect.value) ||
      selectors.preferredAsset(project.assets.filter((item) => item.version === versionSelect.value));
    selectedAsset = selected;
    if (!selected) {
      selectedAsset = null;
      availability.textContent = text("download.unavailable", "暂不可下载");
      availability.className = "project-availability warn";
      fileName.textContent = text("project.noFile", "暂无可下载文件");
      fileMeta.textContent = "";
      button.disabled = true;
      delete button.dataset.downloadPath;
      button.removeAttribute("title");
      return;
    }
    const available = !!selected.available;
    availability.textContent = available ? text("download.available", "可下载") :
      text("download.unavailable", "暂不可下载");
    availability.className = "project-availability " + (available ? "ok" : "warn");
    fileName.textContent = selected.file_name || text("project.unnamedFile", "未命名文件");
    fileMeta.textContent = selected.version + " · " + selectors.bytesText(selected.size_bytes);
    button.disabled = !available;
    button.dataset.downloadPath = selected.download_path || "";
    if (selected.unavailable_reason) button.title = selected.unavailable_reason;
    else button.removeAttribute("title");
  }

  function setMode(nextMode) {
    if (!project) return;
    mode = nextMode === "file" ? "file" : "selectors";
    selectors.setModeButtons(modeButtons, mode);
    fileBrowser.hidden = mode !== "file";
    versionField.hidden = mode === "file" || versions.length <= 1;
    systemField.hidden = mode !== "selectors" || !systemEnabled;
    archField.hidden = mode !== "selectors" || !architectureEnabled;
    assetSummary.hidden = mode === "file";
    if (mode === "file") browser.showDefault(selectedAsset, versionSelect.value);
    else refreshSystems(selectedAsset);
  }

  function openDownload(path) {
    if (!path) return;
    const target = new URL(path, window.location.href);
    target.searchParams.set("from", "project");
    window.location.href = target.pathname + target.search + target.hash;
  }

  function bindProject(nextProject) {
    project = nextProject;
    systemEnabled = selectorEnabled(project,
      "system_selector_enabled", "system_match_enabled");
    architectureEnabled = selectorEnabled(project,
      "architecture_selector_enabled", "architecture_match_enabled");
    versions = selectors.uniqueVersions(project.assets || []);
    versionSelect.innerHTML = "";
    versions.forEach((version) => {
      const option = document.createElement("option");
      option.value = version;
      option.textContent = version;
      if (version === project.default_version) option.selected = true;
      versionSelect.appendChild(option);
    });
    browser = window.DownloadFileBrowser.create(fileBrowser, project.assets, {
      asset: selectedAsset,
      bytesText: selectors.bytesText,
      onDownload: function (item) { openDownload(item.download_path); },
      onVersion: function (version) { versionSelect.value = version; },
      recommendedAsset: function (items) {
        return selectors.preferredAssetForUser(items, architectureEnabled);
      },
      uniqueVersions: selectors.uniqueVersions,
      version: versionSelect.value
    });
    setStatus(versions.length ? "" : text("project.noFiles", "暂无可展示文件。"),
      versions.length ? "muted" : "warn");
    setMode(selectors.selectionModeForProject(projectId, project.default_selection_mode));
    if (icon) icon.alt = text("project.icon", project.display_name + " 图标", {value: project.display_name});
    if (updated) {
      const value = project.latest_published_at || updated.dataset.updatedAt || "";
      const formatted = value && i18n ? i18n.formatDate(value) : value;
      updated.textContent = formatted ? text("download.projectUpdated", "最近更新：" + formatted, {value: formatted}) : "";
    }
  }

  async function loadProject() {
    try {
      const resp = await fetch("/api/public/v1/catalog", {cache: "default"});
      if (!resp.ok) {
        const error = new Error("项目数据加载失败");
        error.status = resp.status;
        try {
          const body = await resp.json();
          error.code = body.code || "";
          error.message = body.message || error.message;
        } catch (_) {}
        throw error;
      }
      const catalog = await resp.json();
      const projects = Array.isArray(catalog.projects) ? catalog.projects : [];
      const found = projects.find((item) => item.project_id === projectId);
      if (!found) {
        const error = new Error("项目不存在或未启用");
        error.code = "ASSET_NOT_FOUND";
        throw error;
      }
      lastError = null;
      bindProject(found);
    } catch (err) {
      lastError = err;
      setStatus(i18n ? i18n.errorMessage(err, "error.project") : err.message || "项目数据加载失败", "warn");
      button.disabled = true;
    }
  }

  if (i18n) i18n.onChange(function () {
    if (project) {
      if (icon) icon.alt = text("project.icon", project.display_name + " 图标", {value: project.display_name});
      if (updated) {
        const value = project.latest_published_at || updated.dataset.updatedAt || "";
        const formatted = value ? i18n.formatDate(value) : "";
        updated.textContent = formatted ? text("download.projectUpdated", "最近更新：" + formatted, {value: formatted}) : "";
      }
      refreshSystems(selectedAsset);
    } else if (lastError) {
      setStatus(i18n.errorMessage(lastError, "error.project"), "warn");
    }
  });

  versionSelect.addEventListener("change", function () { refreshSystems(); });
  systemSelect.addEventListener("change", function () { refreshArchitectures(); });
  archSelect.addEventListener("change", function () { refreshDetails(); });
  modeButtons.forEach((item) => item.addEventListener("click", function () {
    if (!project || item.dataset.selectionMode === mode) return;
    setMode(item.dataset.selectionMode);
    selectors.rememberSelectionMode(projectId, mode);
  }));
  button.addEventListener("click", function () {
    openDownload(button.dataset.downloadPath);
  });
  loadProject();
})();
