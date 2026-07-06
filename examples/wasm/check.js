// check.js — headless smoke test of the demo's JS API, run by
// tools/check-wasm.sh under Node: loads main.wasm exactly like the
// browser would (wasm_exec.js), then drives the exported z80* functions
// and asserts the hello program runs to HALT with the expected result.
"use strict";
require("./wasm_exec.js");
const fs = require("fs");

// Minimal stand-ins for the browser APIs the demo touches on startup.
globalThis.Event ??= class Event { constructor(type) { this.type = type; } };
globalThis.dispatchEvent ??= () => true;
globalThis.addEventListener ??= () => {};

const go = new Go();
WebAssembly.instantiate(fs.readFileSync(__dirname + "/main.wasm"), go.importObject)
  .then(async (result) => {
    go.run(result.instance); // runs main() until it blocks in select{}
    await new Promise((res) => setTimeout(res, 50));

    const program = new Uint8Array(fs.readFileSync(__dirname + "/hello.bin"));
    z80Load(program, 0x8000, 0x8000);
    const steps = z80Run(1_000_000);
    const st = z80State();
    const msg = Buffer.from(z80Mem(0x9000, 14)).toString("ascii");

    console.log(`steps=${steps} halted=${st.halted} tstates=${st.tstates} msg=${JSON.stringify(msg)}`);
    if (!st.halted || msg !== "HELLO FROM Z80" || st.tstates !== 572) {
      console.error("demo API check: FAILED");
      process.exit(1);
    }
    console.log("demo API check: OK");
    process.exit(0);
  })
  .catch((err) => { console.error(err); process.exit(1); });
