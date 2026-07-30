(function () {
  const source = document.getElementById("download-pow-asset");
  const title = document.getElementById("download-pow-title");
  const meta = document.getElementById("download-pow-meta");
  const statusBox = document.getElementById("download-pow-status");
  const statusCopies = statusBox && statusBox.querySelectorAll(".download-pow__status-copy");
  const retryButton = document.getElementById("download-pow-retry");
  const returnButton = document.querySelector(".download-pow__back");
  if (!source || !title || !meta || !statusBox || !statusCopies ||
      statusCopies.length !== 2 || !retryButton) return;

  const asset = JSON.parse(source.textContent || "{}");
  let running = false;
  let progressFrame = 0;
  let pendingProgressAttempts = 0;
  let pendingProgressDifficulty = 0;

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

  function setStatusContent(message, percent) {
    statusCopies.forEach(function (copy) {
      copy.querySelector(".download-pow__status-message").textContent = message;
      const percentBox = copy.querySelector(".download-pow__status-percent");
      percentBox.textContent = percent === null ? "" : percent + "%";
      percentBox.hidden = percent === null;
    });
  }

  function setStatus(message, level, progress) {
    const active = Number.isFinite(progress);
    const value = active ? Math.max(0, Math.min(100, progress)) : null;
    const percent = active ? Math.round(value) : null;
    statusBox.className = "status " + (level || "muted") + (active ? " status--progress" : "");
    setStatusContent(message, percent);
    statusBox.setAttribute("role", active ? "progressbar" : "status");
    statusBox.setAttribute("aria-live", active ? "off" : "polite");
    if (!active) {
      statusBox.style.removeProperty("--download-pow-progress");
      statusBox.removeAttribute("aria-valuemin");
      statusBox.removeAttribute("aria-valuemax");
      statusBox.removeAttribute("aria-valuenow");
      statusBox.removeAttribute("aria-valuetext");
      statusBox.removeAttribute("aria-label");
      return;
    }
    statusBox.style.setProperty("--download-pow-progress", value + "%");
    statusBox.setAttribute("aria-valuemin", "0");
    statusBox.setAttribute("aria-valuemax", "100");
    statusBox.setAttribute("aria-valuenow", String(percent));
    statusBox.setAttribute("aria-valuetext", percent + "%");
    statusBox.setAttribute("aria-label", message);
  }

  function estimatedProgress(attempts, difficulty) {
    const bits = Math.max(1, Math.floor(Number(difficulty) || 0));
    const chancePerAttempt = Math.pow(2, -bits);
    const target = Math.ceil(Math.log(0.2) / Math.log1p(-chancePerAttempt));
    const count = Math.max(0, Number(attempts) || 0);
    if (count <= target) return 95 * count / target;
    return Math.min(99, 95 + 4 * (1 - Math.exp(-(count - target) / target)));
  }

  function queueCalculationProgress(attempts, difficulty) {
    pendingProgressAttempts = attempts;
    pendingProgressDifficulty = difficulty;
    if (progressFrame) return;
    progressFrame = window.requestAnimationFrame(function () {
      progressFrame = 0;
      setStatus("正在计算验证答案...", "muted",
        estimatedProgress(pendingProgressAttempts, pendingProgressDifficulty));
    });
  }

  function cancelProgressUpdate() {
    if (!progressFrame) return;
    window.cancelAnimationFrame(progressFrame);
    progressFrame = 0;
    pendingProgressAttempts = 0;
    pendingProgressDifficulty = 0;
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
      setStatus(asset.unavailable_reason, "warn");
      return;
    }
    if (!window.crypto || !window.crypto.subtle || !window.PowSolver) {
      setStatus(powStartupMessage(), "warn status--strong");
      return;
    }
    running = true;
    try {
      setStatus("正在创建下载挑战...", "muted");
      const challengeResp = await postJSON("/api/public/v1/web/challenges", {asset_id: asset.asset_id});
      const challengeData = challengeResp.data || {};
      const altcha = challengeData.altcha || {};
      if (!challengeData.challenge_id || !altcha.challenge) throw new Error("挑战数据缺失");
      const difficulty = challengeData.difficulty || 10;
      setStatus("正在计算验证答案...", "muted", 0);
      let number;
      try {
        number = await window.PowSolver.solve(altcha.challenge, difficulty, {
          onProgress: function (attempts) {
            queueCalculationProgress(attempts, difficulty);
          }
        });
      } catch (err) {
        throw new Error(powStartupMessage());
      }
      cancelProgressUpdate();
      setStatus("验证计算完成，正在签发并同步下载令牌...", "muted", 100);
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
      cancelProgressUpdate();
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
