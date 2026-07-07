// gozilog — cycle-accurate Z80 CPU emulator, Go compiled to WebAssembly.
// Works in browsers and Node (ESM). One WASM instance is shared;
// createZ80() hands out independent CPU instances.
import "./wasm_exec.js";

let apiPromise = null;

async function loadWasmBytes() {
  const url = new URL("./gozilog.wasm", import.meta.url);
  if (url.protocol === "file:") {
    // Node: import.meta.url is a file URL; fetch() can't read those.
    const { readFile } = await import("node:fs/promises");
    const { fileURLToPath } = await import("node:url");
    return await readFile(fileURLToPath(url));
  }
  const resp = await fetch(url);
  if (!resp.ok) {
    throw new Error(`gozilog: fetch gozilog.wasm failed: HTTP ${resp.status}`);
  }
  return await resp.arrayBuffer();
}

function bootstrap() {
  if (!apiPromise) {
    apiPromise = (async () => {
      const go = new Go();
      const { instance } = await WebAssembly.instantiate(
        await loadWasmBytes(),
        go.importObject,
      );
      go.run(instance); // runs main() until it blocks; never resolves
      const make = globalThis.__gozilog_new;
      if (typeof make !== "function") {
        throw new Error("gozilog: wasm module failed to initialize");
      }
      return make;
    })();
  }
  return apiPromise;
}

/**
 * Create a Z80 CPU with 64K of flat RAM (I/O reads return 0xFF).
 * @returns {Promise<import("./index.d.ts").Z80>}
 */
export async function createZ80() {
  const make = await bootstrap();
  return make();
}
