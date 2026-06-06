(function () {
  const statusBox = document.getElementById("download-status");
  const container = document.getElementById("project-cards");
  const source = document.getElementById("download-projects");
  const cardTemplate = document.getElementById("project-card-template");
  if (!statusBox || !container || !source || !cardTemplate) return;
  const projects = JSON.parse(source.textContent || "[]");
  const selectors = window.DownloadSelectors || {
    preferredAsset: (items) => items.find((item) => item.available) || items[0] || null,
    preferredAssetForUser: (items) => items.find((item) => item.available) || items[0] || null,
    userSystem: () => ""
  };

  function setStatus(message, level) {
    statusBox.textContent = message;
    statusBox.className = "status " + (level || "muted");
    statusBox.hidden = !message;
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

  function preferredAsset(items) { return selectors.preferredAsset(items); }
  function preferredAssetForUser(items, useArchitecture) {
    return selectors.preferredAssetForUser(items, useArchitecture);
  }

  function buildCard(project) {
    const card = cardTemplate.content.firstElementChild.cloneNode(true);
    const versions = uniqueVersions(project.assets);
    const defaultVersion = project.default_version || (versions[0] || "");
    card.querySelector(".project-card__icon").src = project.icon_url;
    card.querySelector(".project-card__icon").alt = project.display_name + " 图标";
    card.querySelector(".project-name").textContent = project.display_name;
    card.querySelector(".project-repository").textContent = project.repository;
    card.querySelector(".project-updated").textContent = "最近更新：" + (project.latest_published_at || "暂无");
    const availability = card.querySelector(".project-availability");
    const versionSelect = card.querySelector(".version-select");
    const systemField = card.querySelector(".system-field");
    const systemSelect = card.querySelector(".system-select");
    const archField = card.querySelector(".architecture-field");
    const archSelect = card.querySelector(".architecture-select");
    const sizeText = card.querySelector(".project-card__size");
    const button = card.querySelector(".download-button");
    const badge = card.querySelector(".version-badge");

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

    function refreshArchitectures() {
      let list = project.assets.filter((item) => item.version === versionSelect.value);
      if (project.system_match_enabled) {
        list = list.filter((item) => item.system === systemSelect.value);
      }
      const choice = preferredAssetForUser(list, !!project.architecture_match_enabled);
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

    function refreshDetails(preferred) {
      const selected = preferred ||
        project.assets.find((item) => item.asset_id === archSelect.value) ||
        preferredAsset(project.assets);
      badge.textContent = selected ? " " + selected.version : "";
      if (!selected) {
        setAvailability(null);
        sizeText.textContent = "暂无可下载文件";
        sizeText.className = "project-card__size warn";
        button.disabled = true;
        return;
      }
      setAvailability(selected);
      sizeText.textContent = bytesText(selected.size_bytes);
      sizeText.className = "project-card__size " + (selected.available ? "muted" : "warn");
      button.disabled = !selected.available;
      button.dataset.assetId = selected.asset_id;
      button.dataset.downloadPath = selected.download_path || "";
      if (selected.unavailable_reason) button.title = selected.unavailable_reason;
      else button.removeAttribute("title");
    }

    if (project.system_match_enabled) systemField.hidden = false;
    if (project.architecture_match_enabled) archField.hidden = false;
    versionSelect.addEventListener("change", refreshSystems);
    systemSelect.addEventListener("change", refreshArchitectures);
    archSelect.addEventListener("change", function () { refreshDetails(); });
    button.addEventListener("click", function () { startDownload(button); });
    refreshSystems();
    return card;
  }

  function startDownload(button) {
    const assetId = button.dataset.assetId;
    const downloadPath = button.dataset.downloadPath;
    if (!assetId || !downloadPath) {
      setStatus("下载资产缺失，请刷新后重试。", "warn");
      return;
    }
    window.location.href = downloadPath;
  }

  projects.forEach((project) => container.appendChild(buildCard(project)));
})();
