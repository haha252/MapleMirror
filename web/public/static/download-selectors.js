(function () {
  const selectionModeKeyPrefix = "mirror-selection-mode:v2:";

  function browserText() {
    return [navigator.userAgentData && navigator.userAgentData.platform,
      navigator.userAgentData && navigator.userAgentData.architecture,
      navigator.platform, navigator.userAgent].filter(Boolean).join(" ").toLowerCase();
  }

  function userSystem() {
    const values = browserText();
    if (/\bharmony(?:os)?\b|openharmony|\bohos\b/.test(values)) return "harmony";
    if (/windows|win32|win64|wow64/.test(values)) return "win";
    if (/linux/.test(values)) return "linux";
    if (/darwin|mac|os x/.test(values)) return "darwin";
    return "";
  }

  function userArchitecture() {
    const values = browserText();
    if (/arm64|aarch64|armv8/.test(values)) return "arm64";
    if (/amd64|x86_64|x64|wow64|win64/.test(values)) return "amd64";
    if (/x86|i386|i686|win32/.test(values)) return "x86";
    return "";
  }

  function normalizeArch(value) {
    const text = String(value || "").toLowerCase();
    if (/arm64|aarch64|armv8/.test(text)) return "arm64";
    if (/amd64|x86_64|x64|64-bit|64bit/.test(text)) return "amd64";
    if (/x86|i386|i686|32-bit|32bit/.test(text)) return "x86";
    if (/\ball\b|universal|any/.test(text)) return "all";
    return text.trim();
  }

  function preferredAsset(items) {
    return items.find((item) => item.available) || items[0] || null;
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
    return Array.from(new Set(items.map((item) => item.version)));
  }

  function setModeButtons(buttons, mode) {
    buttons.forEach((button) => {
      const active = button.dataset.selectionMode === mode;
      button.setAttribute("aria-pressed", String(active));
    });
  }

  function normalizeSelectionMode(mode) {
    return String(mode || "").toLowerCase().trim() === "file" ? "file" : "selectors";
  }

  function selectionModeForProject(projectId, fallback) {
    if (!projectId) return normalizeSelectionMode(fallback);
    try {
      const stored = window.localStorage ?
        localStorage.getItem(selectionModeKeyPrefix + projectId) : "";
      if (stored === "selectors" || stored === "file") return stored;
    } catch (_) {
      // localStorage may be unavailable in private or restricted browser contexts.
    }
    return normalizeSelectionMode(fallback);
  }

  function rememberSelectionMode(projectId, mode) {
    if (!projectId) return;
    try {
      if (window.localStorage) {
        localStorage.setItem(selectionModeKeyPrefix + projectId, normalizeSelectionMode(mode));
      }
    } catch (_) {
      // The current page selection still works when persistence is unavailable.
    }
  }

  function preferredAssetForUser(items, useArchitecture) {
    const available = items.filter((item) => item.available);
    const list = available.length ? available : items;
    const wantedSystem = userSystem();
    const systemList = wantedSystem ?
      list.filter((item) => String(item.system || "") === wantedSystem) : [];
    const systemMatched = systemList.length ? systemList : list;
    if (!useArchitecture) return preferredAsset(systemMatched);
    const wantedArch = userArchitecture();
    return systemMatched.find((item) => normalizeArch(item.architecture) === wantedArch) ||
      systemMatched.find((item) => normalizeArch(item.architecture) === "all") ||
      systemMatched.find((item) => !String(item.architecture || "").trim()) ||
      preferredAsset(systemMatched);
  }

  function systemLabel(value) {
    const labels = {
      win: "Windows",
      linux: "Linux",
      darwin: "macOS",
      harmony: "鸿蒙",
      None: "未识别"
    };
    return labels[value] || String(value || "");
  }

  window.DownloadSelectors = {
    bytesText: bytesText,
    preferredAsset: preferredAsset,
    preferredAssetForUser: preferredAssetForUser,
    rememberSelectionMode: rememberSelectionMode,
    selectionModeForProject: selectionModeForProject,
    setModeButtons: setModeButtons,
    uniqueVersions: uniqueVersions,
    userSystem: userSystem,
    systemLabel: systemLabel
  };
})();
