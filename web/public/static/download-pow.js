(function () {
  const source = document.getElementById("download-pow-asset");
  const title = document.getElementById("download-pow-title");
  const meta = document.getElementById("download-pow-meta");
  const statusBox = document.getElementById("download-pow-status");
  const retryButton = document.getElementById("download-pow-retry");
  const returnButton = document.querySelector(".download-pow__back");
  if (!source || !title || !meta || !statusBox || !retryButton) return;

  const asset = JSON.parse(source.textContent || "{}");
  let running = false;

  function bytesText(value) {
    const size = Number(value) || 0;
    if (size <= 0) return "";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let scaled = size;
    let unit = 0;
    while (scaled >= 1024 && unit < units.length - 1) {
      scaled = scaled / 1024;
      unit++;
    }
    return (unit === 0 ? String(scaled) : scaled.toFixed(2)) + " " + units[unit];
  }

  function showText(element, value) {
    const text = String(value || "").trim();
    element.textContent = text;
    element.hidden = text === "";
  }

  function setStatus(message, level) {
    statusBox.textContent = message;
    statusBox.className = "status " + (level || "muted");
  }

  function powStartupMessage() {
    return "PoW 验证组件启动失败。请升级当前浏览器，或更换为新版 Chrome、Edge、Firefox、Safari 后重试。";
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

  function downloadURL(data) {
    const target = new URL(data.download_url, window.location.href);
    target.searchParams.set("token", data.download_token);
    return target.toString();
  }

  function safeReferrerURL() {
    const raw = String(document.referrer || "").trim();
    if (!raw) return "";
    try {
      const target = new URL(raw);
      if (target.protocol !== "http:" && target.protocol !== "https:") return "";
      if (target.href === window.location.href) return "";
      return target.href;
    } catch (err) {
      return "";
    }
  }

  function fallbackTo(referrerURL) {
    window.location.assign(referrerURL || "/");
  }

  function likelyOpenedInNewTab(referrerURL) {
    return Boolean(window.opener) || Boolean(referrerURL && window.history.length <= 1);
  }

  function closeWithFallback(referrerURL) {
    const timer = window.setTimeout(function () {
      fallbackTo(referrerURL);
    }, 300);
    window.addEventListener("pagehide", function () {
      window.clearTimeout(timer);
    }, {once: true});
    window.close();
  }

  function backWithFallback(referrerURL) {
    if (window.history.length <= 1) {
      fallbackTo(referrerURL);
      return;
    }
    const timer = window.setTimeout(function () {
      fallbackTo(referrerURL);
    }, 700);
    window.addEventListener("pagehide", function () {
      window.clearTimeout(timer);
    }, {once: true});
    window.history.back();
  }

  async function start() {
    if (running) return;
    retryButton.hidden = true;
    if (!asset.available) {
      setStatus(asset.unavailable_reason || "该文件暂不可下载。", "warn");
      retryButton.hidden = false;
      return;
    }
    if (!window.crypto || !window.crypto.subtle || !window.PowSolver) {
      setStatus(powStartupMessage(), "warn status--strong");
      retryButton.hidden = false;
      return;
    }
    running = true;
    try {
      setStatus("正在创建下载挑战...", "muted");
      const challengeResp = await postJSON("/api/public/v1/web/challenges", {asset_id: asset.asset_id});
      const challengeData = challengeResp.data || {};
      const altcha = challengeData.altcha || {};
      if (!challengeData.challenge_id || !altcha.challenge) throw new Error("挑战数据缺失");
      setStatus("正在计算验证答案...", "muted");
      let number;
      try {
        number = await window.PowSolver.solve(altcha.challenge, challengeData.difficulty || 10);
      } catch (err) {
        throw new Error(powStartupMessage());
      }
      setStatus("正在签发并同步下载令牌...", "muted");
      const authResp = await postJSON("/api/public/v1/web/authorizations", {
        challenge_id: challengeData.challenge_id,
        asset_id: asset.asset_id,
        altcha_payload: {number: number}
      });
      const authData = authResp.data || {};
      if (!authData.download_url || !authData.download_token) throw new Error("授权数据缺失");
      setStatus("令牌签发完成，正在开始下载。", "ok");
      window.location.assign(downloadURL(authData));
    } catch (err) {
      setStatus(err && err.message ? err.message : "下载失败", "warn");
      retryButton.hidden = false;
    } finally {
      running = false;
    }
  }

  showText(title, asset.project_name);
  showText(meta, [
    asset.version,
    asset.system,
    asset.architecture,
    bytesText(asset.size_bytes)
  ].filter(Boolean).join(" / "));
  retryButton.addEventListener("click", start);
  if (returnButton) {
    const referrerURL = safeReferrerURL();
    const openedInNewTab = likelyOpenedInNewTab(referrerURL);
    returnButton.textContent = openedInNewTab ? "关闭并返回来源页" : "返回上一页";
    returnButton.addEventListener("click", function () {
      if (openedInNewTab) closeWithFallback(referrerURL);
      else backWithFallback(referrerURL);
    });
  }
  start();
})();
