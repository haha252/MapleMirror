(function () {
  const encoder = new TextEncoder();
  let wasmBytesPromise;
  let workerURL;

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

  function workerCount(limit) {
    const count = Math.max(1, Math.floor(navigator.hardwareConcurrency || 4));
    const cap = Number(limit);
    const workerCap = Number.isFinite(cap) && cap > 0 ? cap : 32;
    return Math.max(1, Math.min(count, workerCap, 32));
  }

  function loadWASMBytes() {
    if (!wasmBytesPromise) {
      const wasmURL = window.MirrorStatic && window.MirrorStatic["pow.wasm"] || "/static/public/pow.wasm";
      wasmBytesPromise = fetch(wasmURL)
        .then((resp) => {
          if (!resp.ok) throw new Error("wasm not found");
          return resp.arrayBuffer();
        });
    }
    return wasmBytesPromise;
  }

  function makeWorkerURL() {
    if (workerURL) return workerURL;
    const source = `
      function writeInput(memory, ptr, text) {
        const bytes = new TextEncoder().encode(text);
        if (bytes.length > 120) throw new Error("challenge too long");
        new Uint8Array(memory.buffer, ptr, bytes.length).set(bytes);
        return bytes.length;
      }
      function readCString(memory, ptr) {
        const bytes = new Uint8Array(memory.buffer, ptr, 32);
        let end = 0;
        while (end < bytes.length && bytes[end] !== 0) end++;
        return new TextDecoder().decode(bytes.slice(0, end));
      }
      self.onmessage = async (event) => {
        const data = event.data || {};
        try {
          const loaded = await WebAssembly.instantiate(data.wasmBytes, {});
          const exports = loaded.instance.exports;
          const ptr = exports.get_buffer();
          const inputLen = writeInput(exports.memory, ptr, data.challenge + ":");
          let start = BigInt(data.start || 0);
          const step = BigInt(data.step || 1);
          const batch = Number(data.batch) || 32768;
          for (;;) {
            const tried = exports.solve_pow(inputLen, data.difficulty, start, step, batch);
            if (tried < 0) throw new Error("invalid pow input");
            if (tried > 0) {
              self.postMessage({type: "found", nonce: readCString(exports.memory, ptr)});
              return;
            }
            start += step * BigInt(batch);
            await new Promise((resolve) => setTimeout(resolve, 0));
          }
        } catch (err) {
          self.postMessage({type: "error", message: err && err.message ? err.message : "wasm failed"});
        }
      };
    `;
    workerURL = URL.createObjectURL(new Blob([source], {type: "text/javascript"}));
    return workerURL;
  }

  async function solveWithWorkers(challenge, difficulty, workerLimit) {
    if (!window.Worker) return Promise.reject(new Error("worker unavailable"));
    const wasmBytes = await loadWASMBytes();
    return new Promise((resolve, reject) => {
      const total = workerCount(workerLimit);
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
        const worker = new Worker(makeWorkerURL());
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
          batch: 262144,
          wasmBytes
        });
      }
    });
  }

  window.PowSolver = {
    threads: workerCount,
    async solve(challenge, difficulty, options) {
      const workerLimit = typeof options === "number" ? options : options && options.workerLimit;
      try {
        return await solveWithWorkers(challenge, difficulty, workerLimit);
      } catch (err) {
        console.warn("PoW worker failed; falling back to single-threaded Web Crypto.", err);
        return solveWithSubtle(challenge, difficulty);
      }
    }
  };
})();
