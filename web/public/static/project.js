(function () {
  const page = document.querySelector(".project-page[data-project-id]");
  if (!page) return;
  const projectId = page.dataset.projectId || "";
  const statusBox = document.getElementById("project-download-status");
  const availability = document.getElementById("project-availability");
  const versionField = document.getElementById("project-version-field");
  const versionSelect = document.getElementById("project-version");
  const systemField = document.getElementById("project-system-field");
  const systemSelect = document.getElementById("project-system");
  const archField = document.getElementById("project-architecture-field");
  const archSelect = document.getElementById("project-architecture");
  const fileBrowser = document.getElementById("project-file-browser");
  const fileName = document.getElementById("project-file-name");
  const fileMeta = document.getElementById("project-file-meta");
  const button = document.getElementById("project-download-button");
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

  function selectorEnabled(item, field, legacyField) {
    if (item[field] != null) return !!item[field];
    return !!item[legacyField];
  }

  function setStatus(message, level) {
    statusBox.textContent = message || "";
    statusBox.className = level || "muted";
  }

  function architectureLabel(item) {
    return String(item.architecture || "").trim() || "None";
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
      availability.textContent = "暂不可下载";
      availability.className = "project-availability warn";
      fileName.textContent = "暂无可下载文件";
      fileMeta.textContent = "";
      button.disabled = true;
      delete button.dataset.downloadPath;
      button.removeAttribute("title");
      return;
    }
    const available = !!selected.available;
    availability.textContent = available ? "可下载" : "暂不可下载";
    availability.className = "project-availability " + (available ? "ok" : "warn");
    fileName.textContent = selected.file_name || "未命名文件";
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
    setStatus(versions.length ? "" : "暂无可展示文件。", versions.length ? "muted" : "warn");
    setMode(selectors.selectionModeForProject(projectId, project.default_selection_mode));
  }

  async function loadProject() {
    try {
      const resp = await fetch("/api/public/v1/catalog", {cache: "default"});
      if (!resp.ok) throw new Error("项目数据加载失败");
      const catalog = await resp.json();
      const projects = Array.isArray(catalog.projects) ? catalog.projects : [];
      const found = projects.find((item) => item.project_id === projectId);
      if (!found) throw new Error("项目不存在或未启用");
      bindProject(found);
    } catch (err) {
      setStatus(err.message || "项目数据加载失败", "warn");
      button.disabled = true;
    }
  }

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
