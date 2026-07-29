(function () {
  const statusBox = document.getElementById("download-status");
  const container = document.getElementById("project-cards");
  const cardTemplate = document.getElementById("project-card-template");
  if (!statusBox || !container || !cardTemplate) return;
  const selectors = window.DownloadSelectors || {
    bytesText: (value) => String(value || 0) + " B",
    preferredAsset: (items) => items.find((item) => item.available) || items[0] || null,
    preferredAssetForUser: (items) => items.find((item) => item.available) || items[0] || null,
    setModeButtons: () => {},
    uniqueVersions: (items) => Array.from(new Set(items.map((item) => item.version))),
    userSystem: () => ""
  };

  function setStatus(message, level) {
    statusBox.textContent = message;
    statusBox.className = "status " + (level || "muted");
    statusBox.hidden = !message;
  }

  function retryButton() {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = "重试";
    button.addEventListener("click", loadCatalog);
    return button;
  }

  function preferredAsset(items) { return selectors.preferredAsset(items); }
  function preferredAssetForUser(items, useArchitecture) {
    return selectors.preferredAssetForUser(items, useArchitecture);
  }

  function selectorEnabled(project, field, legacyField) {
    if (project[field] != null) return !!project[field];
    return !!project[legacyField];
  }

  function buildCard(project) {
    const card = cardTemplate.content.firstElementChild.cloneNode(true);
    const versions = selectors.uniqueVersions(project.assets);
    const defaultVersion = project.default_version || (versions[0] || "");
    const projectHref = "/" + encodeURIComponent(project.project_id || "") + "/";
    const icon = card.querySelector(".project-card__icon");
    const link = card.querySelector(".project-link");
    icon.src = project.icon_url;
    icon.alt = project.display_name + " 图标";
    icon.addEventListener("click", function () { window.location.href = projectHref; });
    icon.tabIndex = 0;
    icon.addEventListener("keydown", function (event) {
      if (event.key === "Enter" || event.key === " ") window.location.href = projectHref;
    });
    link.href = projectHref;
    card.querySelector(".project-name").textContent = project.display_name;
    card.querySelector(".project-repository").textContent = project.repository;
    card.querySelector(".project-updated").textContent = "最近更新：" + (project.latest_published_at || "暂无");
    const availability = card.querySelector(".project-availability");
    const versionField = card.querySelector(".version-field");
    const versionSelect = card.querySelector(".version-select");
    const systemField = card.querySelector(".system-field");
    const systemSelect = card.querySelector(".system-select");
    const archField = card.querySelector(".architecture-field");
    const archSelect = card.querySelector(".architecture-select");
    const fileBrowser = card.querySelector(".file-browser");
    const modeButtons = Array.from(card.querySelectorAll("[data-selection-mode]"));
    const sizeText = card.querySelector(".project-card__size");
    const button = card.querySelector(".download-button");
    const badge = card.querySelector(".version-badge");
    const systemEnabled = selectorEnabled(project,
      "system_selector_enabled", "system_match_enabled");
    const architectureEnabled = selectorEnabled(project,
      "architecture_selector_enabled", "architecture_match_enabled");
    let mode = "selectors";
    let selectedAsset = null;
    let browser = null;

    function architectureLabel(item) {
      return String(item.architecture || "").trim() || "None";
    }

    function setAvailability(selected) {
      const available = !!(selected && selected.available);
      availability.textContent = available ? "可下载" : "暂不可下载";
      availability.className = "project-availability " + (available ? "ok" : "warn");
    }

    versions.forEach((version) => {
      const option = document.createElement("option");
      option.value = version;
      option.textContent = version;
      if (version === defaultVersion) option.selected = true;
      versionSelect.appendChild(option);
    });

    function refreshArchitectures(preferred) {
      let list = project.assets.filter((item) => item.version === versionSelect.value);
      if (systemEnabled) {
        list = list.filter((item) => item.system === systemSelect.value);
      }
      const choice = list.includes(preferred) ? preferred :
        preferredAssetForUser(list, architectureEnabled);
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

    function refreshDetails(preferred) {
      const selected = preferred ||
        project.assets.find((item) => item.asset_id === archSelect.value) ||
        preferredAsset(project.assets.filter((item) => item.version === versionSelect.value));
      selectedAsset = selected;
      badge.textContent = selected ? " " + selected.version : "";
      if (!selected) {
        selectedAsset = null;
        setAvailability(null);
        sizeText.textContent = "暂无可下载文件";
        sizeText.className = "project-card__size warn";
        button.disabled = true;
        delete button.dataset.assetId;
        delete button.dataset.downloadPath;
        button.removeAttribute("title");
        return;
      }
      setAvailability(selected);
      sizeText.textContent = selectors.bytesText(selected.size_bytes);
      sizeText.className = "project-card__size " + (selected.available ? "muted" : "warn");
      button.disabled = !selected.available;
      button.dataset.assetId = selected.asset_id;
      button.dataset.downloadPath = selected.download_path || "";
      if (selected.unavailable_reason) button.title = selected.unavailable_reason;
      else button.removeAttribute("title");
    }

    function setMode(nextMode) {
      mode = nextMode === "file" ? "file" : "selectors";
      selectors.setModeButtons(modeButtons, mode);
      fileBrowser.hidden = mode !== "file";
      versionField.hidden = mode === "file";
      systemField.hidden = mode !== "selectors" || !systemEnabled;
      archField.hidden = mode !== "selectors" || !architectureEnabled;
      if (mode === "file") browser.showVersions(selectedAsset, versionSelect.value);
      else refreshSystems(selectedAsset);
    }

    browser = window.DownloadFileBrowser.create(fileBrowser, project.assets, {
      asset: selectedAsset,
      bytesText: selectors.bytesText,
      onSelect: refreshDetails,
      onVersion: function (version) { versionSelect.value = version; },
      preferredAsset: preferredAsset,
      uniqueVersions: selectors.uniqueVersions,
      version: versionSelect.value
    });
    versionSelect.addEventListener("change", function () { refreshSystems(); });
    systemSelect.addEventListener("change", function () { refreshArchitectures(); });
    archSelect.addEventListener("change", function () { refreshDetails(); });
    modeButtons.forEach((item) => item.addEventListener("click", function () {
      setMode(item.dataset.selectionMode);
    }));
    button.addEventListener("click", function () { startDownload(button); });
    setMode("selectors");
    return card;
  }

  function startDownload(button) {
    const assetId = button.dataset.assetId;
    const downloadPath = button.dataset.downloadPath;
    if (!assetId || !downloadPath) {
      setStatus("下载资产缺失，请刷新后重试。", "warn");
      return;
    }
    window.location.href = homeDownloadHref(downloadPath);
  }

  function homeDownloadHref(downloadPath) {
    const target = new URL(downloadPath, window.location.href);
    target.searchParams.set("from", "home");
    return target.pathname + target.search + target.hash;
  }

  async function loadCatalog() {
    setStatus("正在加载项目列表...", "muted");
    container.innerHTML = "";
    try {
      const resp = await fetch("/api/public/v1/catalog", {cache: "default"});
      if (!resp.ok) throw new Error("项目列表加载失败");
      const catalog = await resp.json();
      const projects = Array.isArray(catalog.projects) ? catalog.projects : [];
      if (!projects.length) {
        setStatus("暂无可展示项目。", "muted");
        return;
      }
      projects.forEach((project) => container.appendChild(buildCard(project)));
      setStatus("", "muted");
    } catch (err) {
      setStatus("项目列表加载失败，请稍后重试。", "warn");
      statusBox.appendChild(document.createTextNode(" "));
      statusBox.appendChild(retryButton());
    }
  }

  loadCatalog();
})();
