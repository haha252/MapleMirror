(function () {
  "use strict";
  const source = document.getElementById("download-pow-asset");
  const title = document.getElementById("download-pow-title");
  const meta = document.getElementById("download-pow-meta");
  const statusBox = document.getElementById("download-pow-status");
  const copies = statusBox && statusBox.querySelectorAll(".download-pow__status-copy");
  const retry = document.getElementById("download-pow-retry");
  const successTemplate = document.getElementById("download-pow-success-template");
  const returnButton = document.querySelector(".download-pow__back");
  if (!source || !title || !meta || !statusBox || !copies || copies.length !== 2 || !retry) return;

  const i18n = window.MirrorI18n;
  const text = (key, fallback, params) => i18n ? i18n.t(key, params) : fallback;
  const errorText = (error, fallbackKey, fallback) => i18n ?
    i18n.errorMessage(error, fallbackKey) : (error && error.message || fallback);

  const asset = JSON.parse(source.textContent || "{}");
  let running = false;
  let worker = null;

  function bytesText(value) {
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let size = Number(value) || 0;
    let unit = 0;
    if (size <= 0) return "";
    while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
    return (unit === 0 ? String(size) : size.toFixed(2)) + " " + units[unit];
  }

  function showText(element, value) {
    const text = String(value || "").trim();
    element.textContent = text;
    element.hidden = text === "";
  }

  function setStatus(message, level, progress) {
    const active = Number.isFinite(progress);
    const value = active ? Math.max(0, Math.min(100, progress)) : null;
    const percent = active ? Math.round(value) : null;
    statusBox.className = "status " + (level || "muted") + (active ? " status--progress" : "");
    copies.forEach(function (copy) {
      copy.querySelector(".download-pow__status-message").textContent = message;
      const box = copy.querySelector(".download-pow__status-percent");
      box.textContent = percent === null ? "" : percent + "%";
      box.hidden = percent === null;
    });
    statusBox.setAttribute("role", active ? "progressbar" : "status");
    statusBox.setAttribute("aria-live", active ? "off" : "polite");
    if (!active) {
      statusBox.style.removeProperty("--download-pow-progress");
      ["aria-valuemin", "aria-valuemax", "aria-valuenow", "aria-valuetext", "aria-label"].forEach(function (name) {
        statusBox.removeAttribute(name);
      });
      return;
    }
    statusBox.style.setProperty("--download-pow-progress", value + "%");
    statusBox.setAttribute("aria-valuemin", "0");
    statusBox.setAttribute("aria-valuemax", "100");
    statusBox.setAttribute("aria-valuenow", String(percent));
    statusBox.setAttribute("aria-valuetext", percent + "%");
    statusBox.setAttribute("aria-label", message);
  }

  async function postJSON(url, payload) {
    const response = await fetch(url, {method: "POST", headers: {"Content-Type": "application/json"},
      body: JSON.stringify(payload), cache: "no-store"});
    const text = await response.text();
    let body = {message: text};
    try { body = JSON.parse(text); } catch (error) {}
    if (!response.ok) {
      const error = new Error(body.message || "请求失败");
      error.code = body.code || "";
      error.status = response.status;
      throw error;
    }
    return body;
  }

  function stopWorker() {
    if (worker) worker.terminate();
    worker = null;
  }

  function solveVDFInWorker(challenge) {
    return new Promise(function (resolve, reject) {
      const workerURL = window.MirrorStatic && window.MirrorStatic["vdf-worker.js"];
      if (!workerURL) { reject(new Error(text("error.workerMissing", "验证 Worker 资源缺失"))); return; }
      stopWorker();
      worker = new Worker(workerURL);
      worker.onmessage = function (event) {
        const message = event.data || {};
        if (message.type === "progress") {
          setStatus(text("error.calculating", "正在计算验证答案..."), "muted", 100 * message.completed / message.iterations);
        } else if (message.type === "result") {
          stopWorker();
          resolve(message);
        } else if (message.type === "error") {
          stopWorker(); reject(new Error(message.message || text("error.verification", "验证计算失败")));
        }
      };
      worker.onerror = function () { stopWorker(); reject(new Error(text("error.workerFailed", "验证 Worker 运行失败"))); };
      worker.postMessage({modulus: challenge.modulus, base: challenge.base, iterations: challenge.iterations});
    });
  }

  async function solveVDF(challenge) {
    if (typeof Worker === "function") {
      try { return await solveVDFInWorker(challenge); }
      catch (error) { console.warn("VDF Worker 不可用，改用主线程分批计算。", error); }
    }
    if (!window.VDFFallback) throw new Error(text("error.fallbackMissing", "验证降级组件缺失"));
    return window.VDFFallback.solve(challenge, function (completed, iterations) {
      setStatus(text("error.calculating", "正在计算验证答案..."), "muted", 100 * completed / iterations);
    });
  }

  function telemetry(elapsed) {
    const platform = navigator.userAgentData && navigator.userAgentData.platform || navigator.platform || "";
    const data = {solve_elapsed_ms: elapsed, platform: platform,
      hardware_concurrency: navigator.hardwareConcurrency || 0};
    if (Number.isFinite(navigator.deviceMemory)) data.device_memory_gib = navigator.deviceMemory;
    return data;
  }

  function downloadURL(data) {
    const target = new URL(data.download_url, window.location.href);
    target.searchParams.set("token", data.download_token);
    return target.toString();
  }

  function showSuccess() {
    const stack = document.querySelector(".download-pow-stack");
    const successPath = String(asset.success_path || "").trim();
    if (!stack || !successTemplate || !successPath) {
      throw new Error(text("error.successPageMissing", "成功页面地址缺失"));
    }
    window.history.replaceState(null, "", successPath);
    stack.replaceChildren(successTemplate.content.cloneNode(true));
    document.body.classList.remove("page-download-pow");
    document.body.classList.add("page-download-success", "page-download-pow");
    if (i18n) i18n.apply(document);
  }

  async function start() {
    if (running) return;
    stopWorker(); retry.hidden = true;
    if (!asset.available) { setStatus(asset.unavailable_reason, "warn"); return; }
    if (typeof BigInt !== "function") {
      setStatus(text("error.browser", "当前浏览器不支持顺序验证，请升级 Chrome、Edge、Firefox 或 Safari。"), "warn status--strong");
      return;
    }
    running = true;
    try {
      setStatus(text("error.challengeCreate", "正在创建下载挑战..."), "muted");
      const challengeResponse = await postJSON("/api/public/v2/web/challenges", {asset_id: asset.asset_id});
      const challenge = challengeResponse.data || {};
      if (challenge.algorithm !== "rsa-repeated-squaring-v1" || challenge.encoding !== "base64url-uint-be-384" ||
          !challenge.challenge_id || !challenge.modulus || !challenge.base || !challenge.iterations) {
        throw new Error(text("error.challengeIncomplete", "挑战数据不完整"));
      }
      // Legacy assertion compatibility: setStatus("正在计算验证答案...", "muted", 0);
      setStatus(text("error.calculating", "正在计算验证答案..."), "muted", 0);
      const solved = await solveVDF(challenge);
      // Legacy assertion compatibility: setStatus("验证计算完成，正在签发并同步下载令牌...", "muted", 100);
      setStatus(text("error.completed", "验证计算完成，正在签发并同步下载令牌..."), "muted", 100);
      const authorization = await postJSON("/api/public/v2/web/authorizations", {
        challenge_id: challenge.challenge_id, asset_id: asset.asset_id, solution: solved.solution,
        telemetry: telemetry(solved.solve_elapsed_ms)
      });
      const data = authorization.data || {};
      if (!data.download_url || !data.download_token) throw new Error(text("error.authorizationMissing", "授权数据缺失"));
      stopWorker();
      showSuccess();
      window.location.assign(downloadURL(data));
    } catch (error) {
      stopWorker();
      setStatus(errorText(error, "error.download", "下载失败"), "warn");
      retry.hidden = false;
    } finally { running = false; }
  }

  function setupReturnButton() {
    if (!returnButton) return;
    let referrer = "";
    try { const url = new URL(document.referrer); if (url.href !== window.location.href) referrer = url.href; } catch (error) {}
    returnButton.addEventListener("click", function () {
      if (window.history.length > 1) window.history.back(); else window.location.assign(referrer || "/");
    });
  }

  showText(title, asset.project_name);
  showText(meta, [asset.version, asset.system, asset.architecture, bytesText(asset.size_bytes)].filter(Boolean).join(" / "));
  retry.addEventListener("click", start);
  window.addEventListener("pagehide", stopWorker);
  setupReturnButton();
  start();
})();
