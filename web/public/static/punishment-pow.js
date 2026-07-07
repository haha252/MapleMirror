(function () {
  const source = document.getElementById("punishment-pow-data");
  const statusBox = document.getElementById("punishment-pow-status");
  const requestID = document.getElementById("punishment-pow-request-id");
  const serverTime = document.getElementById("punishment-pow-server-time");
  const sourceBox = document.getElementById("punishment-pow-source");
  const emailBox = document.getElementById("punishment-pow-email");
  if (!source || !statusBox || !requestID || !serverTime || !sourceBox || !emailBox) return;

  const data = JSON.parse(source.textContent || "{}");
  const difficulty = Number(data.difficulty) || 128;
  const challenge = String(data.challenge_seed || "");

  function setText(node, value) {
    node.textContent = String(value || "-");
  }

  function setStatus(message, strong) {
    statusBox.textContent = message;
    statusBox.className = "status " + (strong ? "status--strong muted" : "muted");
  }

  async function start() {
    setText(requestID, data.request_id || "-");
    setText(serverTime, data.server_time || "-");
    setText(sourceBox, data.source || "-");
    setText(emailBox, data.email || "-");
    if (!window.PowSolver) {
      setStatus("当前浏览器无法启动验证组件。", true);
      return;
    }
    setStatus("正在计算验证答案……", true);
    try {
      const nonce = await window.PowSolver.solve(challenge, difficulty, {
        workerLimit: data.worker_limit
      });
      setStatus("验证计算已完成，本次访问仍处于限制状态。", true);
      if (typeof nonce === "number") {
        void nonce;
      }
    } catch (err) {
      console.warn("punishment pow failed", err);
      setStatus("验证组件启动失败，请稍后重试。", true);
    }
  }

  start();
})();
