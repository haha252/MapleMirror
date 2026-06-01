(function () {
  function browserText() {
    return [navigator.userAgentData && navigator.userAgentData.platform,
      navigator.userAgentData && navigator.userAgentData.architecture,
      navigator.platform, navigator.userAgent].filter(Boolean).join(" ").toLowerCase();
  }

  function userSystem() {
    const values = browserText();
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

  function preferredAssetForUser(items) {
    const available = items.filter((item) => item.available);
    const list = available.length ? available : items;
    const wantedSystem = userSystem();
    const systemList = wantedSystem ?
      list.filter((item) => String(item.system || "") === wantedSystem) : [];
    const systemMatched = systemList.length ? systemList : list;
    const wantedArch = userArchitecture();
    return systemMatched.find((item) => normalizeArch(item.architecture) === wantedArch) ||
      systemMatched.find((item) => normalizeArch(item.architecture) === "all") ||
      systemMatched.find((item) => !String(item.architecture || "").trim()) ||
      preferredAsset(systemMatched);
  }

  window.DownloadSelectors = {
    preferredAsset: preferredAsset,
    preferredAssetForUser: preferredAssetForUser,
    userSystem: userSystem
  };
})();
