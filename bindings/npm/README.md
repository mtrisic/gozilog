# gozilog

A **cycle-accurate Zilog Z80 CPU emulator** for JavaScript — the
[gozilog](https://github.com/mtrisic/gozilog) Go library compiled to
WebAssembly (~120 KB gzipped). Every documented *and* undocumented
opcode: MEMPTR (WZ), undocumented X/Y flags, the Q and P latches,
accurate R register, interrupt modes 0/1/2.

Correctness is not aspirational: the core passes all 1604
[SingleStepTests](https://github.com/SingleStepTests/z80) files
(~1.6 million cases) with per-T-state cycle-trace assertions, ZEXDOC
and ZEXALL report all CRCs OK, and the WASM build shipped here is
differentially verified against the reference build on every release.

Works in browsers and Node (≥18), ESM, TypeScript types included.
[**Live demo**](https://mtrisic.github.io/gozilog/demo/).

## Install

```sh
npm install gozilog
```

## Use

```js
import { createZ80 } from "gozilog";

const cpu = await createZ80(); // 64K flat RAM, I/O reads return 0xFF

// LD A,0x2A; LD (0x9000),A; HALT
const program = new Uint8Array([0x3e, 0x2a, 0x32, 0x00, 0x90, 0x76]);
cpu.load(program, 0x8000, 0x8000); // bytes, org, entry

cpu.run(1_000_000);                // run to HALT (or step budget)
console.log(cpu.state().halted);   // true
console.log(cpu.mem(0x9000, 1)[0]); // 42
```

## API

| Method | Description |
|---|---|
| `createZ80(): Promise<Z80>` | new independent CPU (the WASM module is shared, lazily loaded) |
| `load(bytes, org, entry)` | clear memory, copy program, `PC=entry`, `SP=0xFFFF` |
| `step(): number` | execute one instruction, returns T-states consumed |
| `run(maxSteps): number` | execute until HALT or budget, returns steps |
| `state(): Z80State` | every register incl. `wz` (MEMPTR), `iff1/2`, `im`, `q`, `p`, `tstates` |
| `setState(state)` | restore a full snapshot (save states, test harnesses) |
| `mem(addr, len): Uint8Array` / `write(addr, bytes)` | read/write memory |
| `setINT(active)` / `nmi()` | interrupt lines |
| `halted(): boolean`, `reset()` | |

All registers in `state()` are plain numbers (`af` = A<<8\|F, etc.) —
see `index.d.ts` for the full shape.

## Scope

This package embeds the CPU in the simplest possible machine: 64K of
flat RAM with open-bus I/O. That is ideal for running test programs,
teaching, and CPU-level tooling. Building a *machine* emulator
(Spectrum, Galaksija, …) with custom memory maps, contention, and
CPU-driven video needs the per-T-state bus hooks of the
[Go library](https://pkg.go.dev/github.com/mtrisic/gozilog/z80) —
this JS binding intentionally stays simple.

## License

MIT — see the [repository](https://github.com/mtrisic/gozilog).
