(function () {
  const source = document.getElementById("download-pow-asset");
  const title = document.getElementById("download-pow-title");
  const meta = document.getElementById("download-pow-meta");
  const statusBox = document.getElementById("download-pow-status");
  const retryButton = document.getElementById("download-pow-retry");
  if (!source || !title || !meta || !statusBox || !retryButton) return;

  const asset = JSON.parse(source.textContent || "{}");
  let running = false;

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

  function setStatus(message, level) {
    statusBox.textContent = message;
    statusBox.className = "status " + (level || "muted");
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

  async function start() {
    if (running) return;
    retryButton.hidden = true;
    if (!asset.available) {
      setStatus(asset.unavailable_reason || "该文件暂不可下载。", "warn");
      retryButton.hidden = false;
      return;
    }
    if (!window.crypto || !window.crypto.subtle || !window.PowSolver) {
      setStatus("当前浏览器不支持下载验证所需的加密能力。", "warn");
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
      const number = await window.PowSolver.solve(altcha.challenge, challengeData.difficulty || 10);
      setStatus("正在领取下载授权...", "muted");
      const authResp = await postJSON("/api/public/v1/web/authorizations", {
        challenge_id: challengeData.challenge_id,
        asset_id: asset.asset_id,
        altcha_payload: {number: number}
      });
      const authData = authResp.data || {};
      if (!authData.download_url || !authData.download_token) throw new Error("授权数据缺失");
      setStatus("授权已签发，正在开始下载。", "ok");
      window.location.assign(downloadURL(authData));
    } catch (err) {
      setStatus(err && err.message ? err.message : "下载失败", "warn");
      retryButton.hidden = false;
    } finally {
      running = false;
    }
  }

  title.textContent = asset.file_name || "文件下载";
  meta.textContent = [
    asset.project_name,
    asset.version,
    asset.system,
    asset.architecture,
    bytesText(asset.size_bytes)
  ].filter(Boolean).join(" / ");
  retryButton.addEventListener("click", start);
  start();
})();
