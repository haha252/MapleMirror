(function () {
  "use strict";
  const BYTE_LENGTH = 384;

  function base64urlToBytes(value) {
    if (typeof value !== "string" || value.length !== 512 || !/^[A-Za-z0-9_-]+$/.test(value)) {
      throw new Error("VDF 整数编码不合法");
    }
    const binary = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
    if (binary.length !== BYTE_LENGTH) throw new Error("VDF 整数长度不合法");
    const bytes = new Uint8Array(BYTE_LENGTH);
    for (let index = 0; index < BYTE_LENGTH; index++) bytes[index] = binary.charCodeAt(index);
    return bytes;
  }

  function bytesToBigInt(bytes) {
    let value = BigInt(0);
    const shift = BigInt(8);
    for (let index = 0; index < bytes.length; index++) {
      value = (value << shift) | BigInt(bytes[index]);
    }
    return value;
  }

  function bigIntToBytes(value) {
    const bytes = new Uint8Array(BYTE_LENGTH);
    const mask = BigInt(255);
    const shift = BigInt(8);
    for (let index = BYTE_LENGTH - 1; index >= 0; index--) {
      bytes[index] = Number(value & mask);
      value >>= shift;
    }
    if (value !== BigInt(0)) throw new Error("VDF 答案超出编码范围");
    return bytes;
  }

  function bytesToBase64url(bytes) {
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += 64) {
      binary += String.fromCharCode.apply(null, bytes.subarray(offset, offset + 64));
    }
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function solve(message) {
    const modulus = bytesToBigInt(base64urlToBytes(message.modulus));
    let value = bytesToBigInt(base64urlToBytes(message.base));
    const iterations = Number(message.iterations);
    if (!Number.isSafeInteger(iterations) || iterations <= 0 || modulus <= BigInt(1) ||
        value <= BigInt(0) || value >= modulus) throw new Error("VDF 挑战参数不合法");
    let completed = 0;
    let lastReport = performance.now();
    const started = lastReport;
    while (completed < iterations) {
      const batchEnd = Math.min(iterations, completed + 256);
      while (completed < batchEnd) {
        value = (value * value) % modulus;
        completed++;
      }
      const now = performance.now();
      if (completed === iterations || now - lastReport >= 90) {
        self.postMessage({type: "progress", completed: completed, iterations: iterations});
        lastReport = now;
      }
    }
    self.postMessage({type: "result", solution: bytesToBase64url(bigIntToBytes(value)),
      solve_elapsed_ms: Math.max(1, Math.round(performance.now() - started))});
  }

  self.onmessage = function (event) {
    try { solve(event.data || {}); }
    catch (error) { self.postMessage({type: "error", message: error && error.message || "VDF 求解失败"}); }
  };
})();
