(function () {
  const source = document.getElementById("punishment-pow-data");
  const statusBox = document.getElementById("punishment-pow-status");
  const requestID = document.getElementById("punishment-pow-request-id");
  const serverTime = document.getElementById("punishment-pow-server-time");
  const sourceBox = document.getElementById("punishment-pow-source");
  const emailBox = document.getElementById("punishment-pow-email");
  if (!source || !statusBox || !requestID || !serverTime || !sourceBox || !emailBox) return;

  const i18n = window.MirrorI18n;
  const text = (key, fallback) => i18n ? i18n.t(key) : fallback;

  const data = JSON.parse(source.textContent || "{}");
  const difficulty = Number(data.difficulty) || 128;
  const challenge = String(data.challenge_seed || "");
  let statusKey = "punishment.calculating";

  function setText(node, value) {
    node.textContent = String(value || "-");
  }

  function setStatus(message, strong) {
    statusBox.textContent = message;
    statusBox.className = "status " + (strong ? "status--strong muted" : "muted");
  }

  function setStatusKey(key, fallback, strong) {
    statusKey = key;
    setStatus(text(key, fallback), strong);
  }

  async function start() {
    setText(requestID, data.request_id || "-");
    setText(serverTime, data.server_time || "-");
    setText(sourceBox, data.source || "-");
    setText(emailBox, data.email || "-");
    if (!window.PowSolver) {
      setStatusKey("punishment.noSolver", "当前浏览器无法启动验证组件。", true);
      return;
    }
    setStatusKey("punishment.calculating", "正在计算验证答案……", true);
    try {
      const nonce = await window.PowSolver.solve(challenge, difficulty, {
        workerLimit: data.worker_limit
      });
      setStatusKey("punishment.completed", "验证计算已完成，本次访问仍处于限制状态。", true);
      if (typeof nonce === "number") {
        void nonce;
      }
    } catch (err) {
      console.warn("punishment pow failed", err);
      setStatusKey("punishment.failure", "验证组件启动失败，请稍后重试。", true);
    }
  }

  if (i18n) i18n.onChange(function () { setStatus(text(statusKey), true); });

  start();
})();
