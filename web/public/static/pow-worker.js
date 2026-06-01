let wasmPromise;

function loadWASM() {
  if (!wasmPromise) {
    wasmPromise = fetch("/static/public/pow.wasm")
      .then((resp) => {
        if (!resp.ok) throw new Error("wasm not found");
        return resp.arrayBuffer();
      })
      .then((bytes) => WebAssembly.instantiate(bytes, {}));
  }
  return wasmPromise;
}

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
    const loaded = await loadWASM();
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
