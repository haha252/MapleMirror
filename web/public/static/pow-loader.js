(function () {
  const encoder = new TextEncoder();

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

  async function solveWithSubtle(challenge, difficulty) {
    for (let i = 0; ; i++) {
      const data = encoder.encode(challenge + ":" + i);
      const digest = await crypto.subtle.digest("SHA-256", data);
      if (hasLeadingZeroBits(new Uint8Array(digest), difficulty)) return i;
      if ((i & 1023) === 0) await new Promise((resolve) => setTimeout(resolve, 0));
    }
  }

  function workerCount() {
    const count = Math.max(1, Math.floor(navigator.hardwareConcurrency || 4));
    return Math.min(count, 32);
  }

  function solveWithWorkers(challenge, difficulty) {
    if (!window.Worker) return Promise.reject(new Error("worker unavailable"));
    return new Promise((resolve, reject) => {
      const total = workerCount();
      const workers = [];
      let settled = false;
      let failures = 0;
      function finish(fn, value) {
        if (settled) return;
        settled = true;
        workers.forEach((worker) => worker.terminate());
        fn(value);
      }
      for (let i = 0; i < total; i++) {
        const worker = new Worker("/static/public/pow-worker.js");
        workers.push(worker);
        worker.onmessage = (event) => {
          const data = event.data || {};
          if (data.type === "found") finish(resolve, Number(data.nonce));
          if (data.type === "error") {
            failures++;
            if (failures >= total) finish(reject, new Error(data.message || "wasm unavailable"));
          }
        };
        worker.onerror = () => {
          failures++;
          if (failures >= total) finish(reject, new Error("worker failed"));
        };
        worker.postMessage({
          challenge,
          difficulty,
          start: i,
          step: total,
          batch: 262144
        });
      }
    });
  }

  window.PowSolver = {
    threads: workerCount,
    async solve(challenge, difficulty) {
      try {
        return await solveWithWorkers(challenge, difficulty);
      } catch (err) {
        return solveWithSubtle(challenge, difficulty);
      }
    }
  };
})();
