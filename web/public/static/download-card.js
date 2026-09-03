(function () {
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

  function selectorEnabled(project, field, legacyField) {
    if (project[field] != null) return !!project[field];
    return !!project[legacyField];
  }

  function architectureLabel(value, i18n) {
    const raw = String(value || "").trim();
    return raw || (i18n ? i18n.t("system.unidentified") : "未识别");
  }

  function create(project, options) {
    const card = options.template.content.firstElementChild.cloneNode(true);
    const i18n = window.MirrorI18n;
    const text = (key, fallback, params) => i18n ? i18n.t(key, params) : fallback;
    if (i18n) i18n.apply(card);
    const assets = Array.isArray(project.assets) ? project.assets : [];
    const versions = selectors.uniqueVersions(assets);
    const defaultVersion = project.default_version || versions[0] || "";
    const localePrefix = document.documentElement.lang === "en" ? "/en" : "";
    const projectHref = localePrefix + "/" + encodeURIComponent(project.project_id || "") + "/";
    const icon = card.querySelector(".project-card__icon");
    const link = card.querySelector(".project-link");
    icon.src = project.icon_url;
    icon.alt = text("project.icon", project.display_name + " 图标", {value: project.display_name});
    icon.tabIndex = 0;
    icon.addEventListener("click", () => { window.location.href = projectHref; });
    icon.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") window.location.href = projectHref;
    });
    link.href = projectHref;
    card.querySelector(".project-name").textContent = project.display_name;
    const latestVersionText = defaultVersion ? "最新版本：" + defaultVersion : "暂无版本";
    card.querySelector(".version-badge").textContent = i18n ?
      text(defaultVersion ? "catalog.latestVersion" : "catalog.noVersion", latestVersionText,
        {value: defaultVersion}) : latestVersionText;
    card.querySelector(".project-repository").textContent = project.repository;
    const updated = project.latest_published_at ?
      (i18n ? i18n.formatDate(project.latest_published_at) : project.latest_published_at) :
      text("catalog.noVersion", "暂无");
    card.querySelector(".project-updated").textContent =
      text("download.projectUpdated", "最近更新：" + updated, {value: updated});
    renderTags(card.querySelector(".project-tags"), project.tags, options);

    const versionField = card.querySelector(".version-field");
    const versionSelect = card.querySelector(".version-select");
    const systemField = card.querySelector(".system-field");
    const systemSelect = card.querySelector(".system-select");
    const archField = card.querySelector(".architecture-field");
    const archSelect = card.querySelector(".architecture-select");
    const fileBrowser = card.querySelector(".file-browser");
    const modeButtons = Array.from(card.querySelectorAll("[data-selection-mode]"));
    const sizeText = card.querySelector(".project-card__size");
    const actions = card.querySelector(".project-card__actions");
    const button = card.querySelector(".download-button");
    const systemEnabled = selectorEnabled(project,
      "system_selector_enabled", "system_match_enabled");
    const architectureEnabled = selectorEnabled(project,
      "architecture_selector_enabled", "architecture_match_enabled");
    let mode = "selectors";
    let selectedAsset = null;
    let browser = null;

    versions.forEach((version) => {
      const option = document.createElement("option");
      option.value = version;
      option.textContent = version;
      option.selected = version === defaultVersion;
      versionSelect.appendChild(option);
    });

    function refreshDetails(preferred) {
      const selected = preferred ||
        assets.find((item) => item.asset_id === archSelect.value) ||
        selectors.preferredAsset(assets.filter((item) => item.version === versionSelect.value));
      selectedAsset = selected || null;
      if (!selected) {
        sizeText.textContent = text("project.noFile", "暂无可下载文件");
        sizeText.className = "project-card__size warn";
        button.disabled = true;
        button.textContent = text("download.unavailable", "暂不可下载");
        delete button.dataset.assetId;
        delete button.dataset.downloadPath;
        button.removeAttribute("title");
        return;
      }
      sizeText.textContent = selectors.bytesText(selected.size_bytes);
      sizeText.className = "project-card__size " + (selected.available ? "muted" : "warn");
      button.disabled = !selected.available;
      const availabilityText = selected.available ? "下载" : "暂不可下载";
      button.textContent = i18n ? text(selected.available ? "download.download" :
        "download.unavailable", availabilityText) : availabilityText;
      button.dataset.assetId = selected.asset_id;
      button.dataset.downloadPath = selected.download_path || "";
      if (selected.unavailable_reason) button.title = selected.unavailable_reason;
      else button.removeAttribute("title");
    }

    function preferredForUser(items) {
      return selectors.preferredAssetForUser(items, architectureEnabled);
    }

    function refreshArchitectures(preferred) {
      let list = assets.filter((item) => item.version === versionSelect.value);
      if (systemEnabled) list = list.filter((item) => item.system === systemSelect.value);
      const choice = list.includes(preferred) ? preferred : preferredForUser(list);
      archSelect.innerHTML = "";
      if (!architectureEnabled) {
        refreshDetails(choice);
        return;
      }
      list.forEach((item) => {
        const option = document.createElement("option");
        option.value = item.asset_id;
        option.textContent = architectureLabel(item.architecture, i18n);
        option.selected = !!choice && choice.asset_id === item.asset_id;
        archSelect.appendChild(option);
      });
      refreshDetails(choice);
    }

    function refreshSystems(preferred) {
      if (!systemEnabled) {
        refreshArchitectures(preferred);
        return;
      }
      const list = assets.filter((item) => item.version === versionSelect.value);
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
        option.selected = system === choice;
        systemSelect.appendChild(option);
      });
      refreshArchitectures(preferred);
    }

    function startAssetDownload(asset) {
      if (!asset || !asset.available || !asset.asset_id || !asset.download_path) {
        options.status(text("download.assetMissing", "下载资产缺失，请刷新后重试。"), "warn");
        return;
      }
      window.location.href = homeDownloadHref(asset.download_path);
    }

    function setMode(nextMode) {
      mode = nextMode === "file" ? "file" : "selectors";
      selectors.setModeButtons(modeButtons, mode);
      fileBrowser.hidden = mode !== "file";
      versionField.hidden = mode === "file" || versions.length <= 1;
      systemField.hidden = mode !== "selectors" || !systemEnabled;
      archField.hidden = mode !== "selectors" || !architectureEnabled;
      actions.hidden = mode === "file";
      if (mode === "file") browser.showDefault(selectedAsset, versionSelect.value);
      else refreshSystems(selectedAsset);
    }

    browser = window.DownloadFileBrowser.create(fileBrowser, assets, {
      asset: selectedAsset, bytesText: selectors.bytesText, onDownload: startAssetDownload,
      onVersion: (version) => { versionSelect.value = version; },
      recommendedAsset: preferredForUser, uniqueVersions: selectors.uniqueVersions,
      version: versionSelect.value
    });
    versionSelect.addEventListener("change", () => refreshSystems());
    systemSelect.addEventListener("change", () => refreshArchitectures());
    archSelect.addEventListener("change", () => refreshDetails());
    modeButtons.forEach((item) => item.addEventListener("click", () => {
      if (item.dataset.selectionMode === mode) return;
      setMode(item.dataset.selectionMode);
      selectors.rememberSelectionMode(project.project_id, mode);
    }));
    button.addEventListener("click", () => {
      if (!button.dataset.assetId || !button.dataset.downloadPath) {
        options.status(text("download.assetMissing", "下载资产缺失，请刷新后重试。"), "warn");
        return;
      }
      window.location.href = homeDownloadHref(button.dataset.downloadPath);
    });
    setMode(selectors.selectionModeForProject(
      project.project_id, project.default_selection_mode));
    return card;
  }

  function renderTags(container, tags, options) {
    const search = String(options.search || "").trim().toLowerCase();
    Object.keys(tags || {}).sort().forEach((group) => {
      (tags[group] || []).forEach((value) => {
        const key = String(group).toLowerCase() + "\u0000" + String(value).toLowerCase();
        const label = options.tagLabels.get(key) || value;
        const selected = options.selectedTags.has(key);
        const searchMatched = !!search && (
          String(value).toLowerCase().includes(search) ||
          String(label).toLowerCase() === search
        );
        if (!selected && !searchMatched) return;
        const tag = document.createElement("span");
        tag.className = "project-tag " +
          (selected ? "project-tag--active" : "project-tag--search-match");
        tag.textContent = label;
        tag.title = group + ": " + value;
        container.appendChild(tag);
      });
    });
  }

  function homeDownloadHref(downloadPath) {
    const target = new URL(downloadPath, window.location.href);
    target.searchParams.set("from", "home");
    return target.pathname + target.search + target.hash;
  }

  window.DownloadCardRenderer = {create: create};
})();
