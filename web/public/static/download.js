(function () {
  const statusBox = document.getElementById("download-status");
  const container = document.getElementById("project-cards");
  const source = document.getElementById("download-projects");
  const cardTemplate = document.getElementById("project-card-template");
  const overlay = document.getElementById("challenge-overlay");
  const overlayText = document.getElementById("challenge-overlay-text");
  if (!statusBox || !container || !source || !cardTemplate || !overlay || !overlayText) return;
  const encoder = new TextEncoder();
  const projects = JSON.parse(source.textContent || "[]");
  let downloadFrame = document.getElementById("download-frame");
  if (!downloadFrame) {
    downloadFrame = document.createElement("iframe");
    downloadFrame.id = "download-frame";
    downloadFrame.hidden = true;
    document.body.appendChild(downloadFrame);
  }

  function setStatus(message, level) {
    statusBox.textContent = message;
    statusBox.className = "status " + (level || "muted");
    statusBox.hidden = !message;
  }

  function setOverlay(message, visible) {
    overlayText.textContent = message;
    overlay.hidden = !visible;
  }

  setOverlay("正在准备挑战...", false);

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

  function hasLeadingZeroBits(bytes, bits) {
    for (const byte of bytes) {
      if (bits <= 0) return true;
      if (bits >= 8) {
        if (byte !== 0) return false;
        bits -= 8;
        continue;
      }
      return (byte >> (8 - bits)) === 0;
    }
    return bits <= 0;
  }

  async function solveChallenge(challenge, difficulty) {
    for (let i = 0; ; i++) {
      const data = encoder.encode(challenge + ":" + i);
      const digest = await crypto.subtle.digest("SHA-256", data);
      if (hasLeadingZeroBits(new Uint8Array(digest), difficulty)) return i;
      if ((i & 1023) === 0) await new Promise((resolve) => setTimeout(resolve, 0));
    }
  }

  async function postJSON(url, payload) {
    const resp = await fetch(url, {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload)
    });
    const text = await resp.text();
    let body = {message: text};
    try { body = JSON.parse(text); } catch (err) {}
    if (!resp.ok) throw new Error(body.message || "请求失败");
    return body;
  }

  async function sleep(ms) {
    await new Promise((resolve) => setTimeout(resolve, ms));
  }

  async function probeDownload(url) {
    const target = new URL(url, window.location.href);
    if (target.origin !== window.location.origin) return;
    const resp = await fetch(target.toString(), {method: "HEAD", credentials: "same-origin"});
    if (resp.ok) return;
    throw new Error("下载入口当前没有到达下载节点，请把 /downloads/ 路径反向代理到下载节点。");
  }

  function uniqueVersions(items) {
    const seen = new Set();
    return items.filter((item) => {
      if (seen.has(item.version)) return false;
      seen.add(item.version);
      return true;
    }).map((item) => item.version);
  }

  function preferredAsset(items) {
    return items.find((item) => item.available) || items[0] || null;
  }

  function userArchitecture() {
    const values = [navigator.userAgentData && navigator.userAgentData.platform,
      navigator.userAgentData && navigator.userAgentData.architecture,
      navigator.platform, navigator.userAgent].filter(Boolean).join(" ").toLowerCase();
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

  function preferredAssetForUser(items) {
    const available = items.filter((item) => item.available);
    const list = available.length ? available : items;
    const wanted = userArchitecture();
    return list.find((item) => normalizeArch(item.architecture) === wanted) ||
      list.find((item) => normalizeArch(item.architecture) === "all") ||
      list.find((item) => !String(item.architecture || "").trim()) ||
      preferredAsset(list);
  }

  function buildCard(project) {
    const card = cardTemplate.content.firstElementChild.cloneNode(true);
    const versions = uniqueVersions(project.assets);
    const defaultVersion = (project.assets.find((item) => item.available) || project.assets[0] || {}).version || "";
    card.querySelector(".project-card__icon").src = project.icon_url;
    card.querySelector(".project-card__icon").alt = project.display_name + " 图标";
    card.querySelector(".project-name").textContent = project.display_name;
    card.querySelector(".project-repository").textContent = project.repository;
    card.querySelector(".project-updated").textContent = "最近更新：" + (project.latest_published_at || "暂无");
    const availability = card.querySelector(".project-availability");
    availability.textContent = project.available ? "可下载" : "暂不可下载";
    availability.className = "project-availability " + (project.available ? "ok" : "warn");
    const versionSelect = card.querySelector(".version-select");
    const archSelect = card.querySelector(".architecture-select");
    const sizeText = card.querySelector(".project-card__size");
    const button = card.querySelector(".download-button");
    const badge = card.querySelector(".version-badge");
    versions.forEach((version) => {
      const option = document.createElement("option");
      option.value = version;
      option.textContent = version;
      if (version === defaultVersion) option.selected = true;
      versionSelect.appendChild(option);
    });

    function refreshArchitectures() {
      const list = project.assets.filter((item) => item.version === versionSelect.value);
      const choice = preferredAssetForUser(list);
      archSelect.innerHTML = "";
      list.forEach((item) => {
        const option = document.createElement("option");
        option.value = item.asset_id;
        option.textContent = item.architecture;
        if (choice && choice.asset_id === item.asset_id) option.selected = true;
        archSelect.appendChild(option);
      });
      refreshDetails();
    }

    function refreshDetails() {
      const selected = project.assets.find((item) => item.asset_id === archSelect.value) || preferredAsset(project.assets);
      badge.textContent = selected ? " " + selected.version : "";
      if (!selected) {
        sizeText.textContent = "暂无可下载文件";
        sizeText.className = "project-card__size warn";
        button.disabled = true;
        return;
      }
      sizeText.textContent = bytesText(selected.size_bytes);
      sizeText.className = "project-card__size " + (selected.available ? "muted" : "warn");
      button.disabled = !selected.available;
      button.dataset.assetId = selected.asset_id;
      button.dataset.assetName = selected.file_name;
      button.dataset.downloadUrl = "";
      if (selected.unavailable_reason) button.title = selected.unavailable_reason;
      else button.removeAttribute("title");
    }

    versionSelect.addEventListener("change", refreshArchitectures);
    archSelect.addEventListener("change", refreshDetails);
    button.addEventListener("click", function () { startDownload(button); });
    refreshArchitectures();
    return card;
  }

  async function startDownload(button) {
    if (!window.crypto || !window.crypto.subtle) {
      setStatus("当前浏览器不支持下载验证所需的加密能力。", "warn");
      return;
    }
    const assetId = button.dataset.assetId;
    const assetName = button.dataset.assetName || assetId;
    const oldText = button.textContent;
    button.disabled = true;
    button.textContent = "验证中...";
    setOverlay("正在准备挑战...", false);
    try {
      setOverlay("正在创建挑战...", true);
      setStatus("正在创建下载挑战...", "muted");
      const challengeResp = await postJSON("/api/public/v1/web/challenges", {asset_id: assetId});
      const challengeData = challengeResp.data || {};
      const altcha = challengeData.altcha || {};
      if (!challengeData.challenge_id || !altcha.challenge) throw new Error("挑战数据缺失");
      const challengeStartedAt = Date.now();
      setOverlay("正在完成 ALTCHA 验证，请稍候...", true);
      setStatus("正在计算验证答案...", "muted");
      const number = await solveChallenge(altcha.challenge, challengeData.difficulty || 10);
      const elapsed = Date.now() - challengeStartedAt;
      if (elapsed < 900) await sleep(900 - elapsed);
      setOverlay("正在领取下载授权...", true);
      setStatus("正在领取下载授权...", "muted");
      const authResp = await postJSON("/api/public/v1/web/authorizations", {
        challenge_id: challengeData.challenge_id,
        asset_id: assetId,
        altcha_payload: {number: number}
      });
      const authData = authResp.data || {};
      if (!authData.download_url || !authData.download_token) throw new Error("授权数据缺失");
      const downloadURL = authData.download_url + "?token=" + encodeURIComponent(authData.download_token);
      setOverlay("正在检查下载入口...", true);
      await probeDownload(downloadURL);
      setStatus("授权已签发，正在开始下载 " + assetName + "。", "ok");
      downloadFrame.src = downloadURL;
    } catch (err) {
      setStatus(err && err.message ? err.message : "下载失败", "warn");
      button.disabled = false;
      button.textContent = oldText;
      setOverlay("", false);
      return;
    }
    setOverlay("", false);
    button.disabled = false;
    button.textContent = oldText;
  }

  projects.forEach((project) => container.appendChild(buildCard(project)));
})();
