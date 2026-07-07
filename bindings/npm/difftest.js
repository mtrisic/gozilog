// Differential test harness (repo-only, not shipped in the package):
// executes a deterministic pseudo-random corpus against ONE build of
// gozilog.wasm and dumps the results as JSON. Running it once against
// the Go build and once against the TinyGo build and diffing the
// outputs proves the two toolchains compiled identical semantics.
//
//   node difftest.js <wasm_exec.js> <gozilog.wasm> > out.json
//
// Corpus: 2000 cases of random memory + random full register state,
// stepped 50 instructions each (random bytes exercise prefixed and
// undocumented opcodes densely); final state + a memory checksum are
// recorded.
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const [execJS, wasmPath] = process.argv.slice(2);
if (!wasmPath) {
  console.error("usage: node difftest.js <wasm_exec.js> <gozilog.wasm>");
  process.exit(2);
}

await import(pathToFileURL(execJS));
const go = new Go();
const { instance } = await WebAssembly.instantiate(await readFile(wasmPath), go.importObject);
go.run(instance);
const cpu = globalThis.__gozilog_new();

// mulberry32: tiny seeded PRNG, identical across runs.
function prng(seed) {
  return function () {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const rnd = prng(0x5a80);
const r16 = () => Math.floor(rnd() * 0x10000);
const r8 = () => Math.floor(rnd() * 0x100);

const results = [];
for (let c = 0; c < 2000; c++) {
  const org = r16();
  const block = new Uint8Array(300);
  for (let i = 0; i < block.length; i++) block[i] = r8();
  cpu.load(block, org, org); // clears memory, then we randomize state

  cpu.setState({
    af: r16(), bc: r16(), de: r16(), hl: r16(),
    af2: r16(), bc2: r16(), de2: r16(), hl2: r16(),
    ix: r16(), iy: r16(), sp: r16(), pc: org, wz: r16(),
    i: r8(), r: r8(), im: c % 3, iff1: rnd() < 0.5, iff2: rnd() < 0.5,
    halted: false, eiPending: false, q: r8(), p: c & 1,
  });

  let t = 0;
  for (let s = 0; s < 50; s++) t += cpu.step();

  const st = cpu.state();
  delete st.tstates; // absolute counter differs by prior cases only in theory; drop for safety
  let sum = 0;
  const around = cpu.mem((org - 64) & 0xffff, 428);
  for (const b of around) sum = (sum * 31 + b) >>> 0;
  const stack = cpu.mem((st.sp - 64) & 0xffff, 128);
  for (const b of stack) sum = (sum * 31 + b) >>> 0;
  results.push({ case: c, t, st, sum });
}

console.log(JSON.stringify(results));
