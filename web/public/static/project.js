(function () {
  const page = document.querySelector(".project-page[data-project-id]");
  if (!page) return;
  const projectId = page.dataset.projectId || "";
  const statusBox = document.getElementById("project-download-status");
  const availability = document.getElementById("project-availability");
  const versionSelect = document.getElementById("project-version");
  const systemField = document.getElementById("project-system-field");
  const systemSelect = document.getElementById("project-system");
  const archField = document.getElementById("project-architecture-field");
  const archSelect = document.getElementById("project-architecture");
  const fileName = document.getElementById("project-file-name");
  const fileMeta = document.getElementById("project-file-meta");
  const button = document.getElementById("project-download-button");
  const selectors = window.DownloadSelectors || {
    preferredAsset: (items) => items.find((item) => item.available) || items[0] || null,
    preferredAssetForUser: (items) => items.find((item) => item.available) || items[0] || null,
    userSystem: () => ""
  };
  let project = null;

  function setStatus(message, level) {
    statusBox.textContent = message || "";
    statusBox.className = level || "muted";
  }

  function bytesText(value) {
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let size = Number(value) || 0;
    let unit = 0;
    while (size >= 1024 && unit < units.length - 1) {
      size = size / 1024;
      unit++;
    }
    return (unit === 0 ? String(size) : size.toFixed(2)) + " " + units[unit];
  }

  function uniqueVersions(items) {
    const seen = new Set();
    return items.filter((item) => {
      if (seen.has(item.version)) return false;
      seen.add(item.version);
      return true;
    }).map((item) => item.version);
  }

  function architectureLabel(item) {
    return String(item.architecture || "").trim() || "None";
  }

  function refreshSystems() {
    if (!project.system_match_enabled) {
      refreshArchitectures();
      return;
    }
    const list = project.assets.filter((item) => item.version === versionSelect.value);
    const systems = Array.from(new Set(list.map((item) => item.system).filter(Boolean)));
    const wanted = selectors.userSystem ? selectors.userSystem() : "";
    const choice = systems.includes(wanted) ? wanted : systems[0] || "";
    systemSelect.innerHTML = "";
    systems.forEach((system) => {
      const option = document.createElement("option");
      option.value = system;
      option.textContent = system;
      if (system === choice) option.selected = true;
      systemSelect.appendChild(option);
    });
    refreshArchitectures();
  }

  function refreshArchitectures() {
    let list = project.assets.filter((item) => item.version === versionSelect.value);
    if (project.system_match_enabled) {
      list = list.filter((item) => item.system === systemSelect.value);
    }
    const choice = selectors.preferredAssetForUser(list, !!project.architecture_match_enabled);
    archSelect.innerHTML = "";
    if (!project.architecture_match_enabled) {
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
      selectors.preferredAsset(project.assets);
    if (!selected) {
      availability.textContent = "暂不可下载";
      availability.className = "project-availability warn";
      fileName.textContent = "暂无可下载文件";
      fileMeta.textContent = "";
      button.disabled = true;
      return;
    }
    const available = !!selected.available;
    availability.textContent = available ? "可下载" : "暂不可下载";
    availability.className = "project-availability " + (available ? "ok" : "warn");
    fileName.textContent = selected.file_name || "未命名文件";
    fileMeta.textContent = selected.version + " · " + bytesText(selected.size_bytes);
    button.disabled = !available;
    button.dataset.downloadPath = selected.download_path || "";
    if (selected.unavailable_reason) button.title = selected.unavailable_reason;
    else button.removeAttribute("title");
  }

  function bindProject(nextProject) {
    project = nextProject;
    const versions = uniqueVersions(project.assets || []);
    versionSelect.innerHTML = "";
    versions.forEach((version) => {
      const option = document.createElement("option");
      option.value = version;
      option.textContent = version;
      if (version === project.default_version) option.selected = true;
      versionSelect.appendChild(option);
    });
    systemField.hidden = !project.system_match_enabled;
    archField.hidden = !project.architecture_match_enabled;
    setStatus(versions.length ? "" : "暂无可展示文件。", versions.length ? "muted" : "warn");
    refreshSystems();
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

  versionSelect.addEventListener("change", refreshSystems);
  systemSelect.addEventListener("change", refreshArchitectures);
  archSelect.addEventListener("change", function () { refreshDetails(); });
  button.addEventListener("click", function () {
    const path = button.dataset.downloadPath;
    if (!path) return;
    const target = new URL(path, window.location.href);
    target.searchParams.set("from", "project");
    window.location.href = target.pathname + target.search + target.hash;
  });
  loadProject();
})();
