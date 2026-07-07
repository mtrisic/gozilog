# gozilog — Z80 CPU emulator library in Go: specification

This is the working specification: it records the design, the timing
model, and the decisions made along the way. Update it when decisions
change.

## Goal

A cycle-accurate, machine-agnostic Zilog Z80 CPU library in Go, including
undocumented behavior: MEMPTR (WZ), undocumented X/Y flags (bits 3/5),
undocumented opcodes, accurate R register, interrupt modes 0/1/2, and
(lower priority) "special RESET".

The first machine built on it will be a **Galaksija** emulator (Z80A-based
Yugoslav home computer). Galaksija generates video with the CPU: hardware
latches character codes off the data bus during M1 cycles while the CPU
executes out of screen RAM, and uses the refresh address (I<<8|R) for
character-row addressing. The library therefore exposes per-T-state timing
with address/data/pin visibility — this is a foundational requirement, not
an add-on. The library itself stays generic (a ZX Spectrum, with its
contended memory, must be implementable on the same API).

## Decisions log

- Module `github.com/mtrisic/gozilog`, library in subpackage `z80/`
  (import `github.com/mtrisic/gozilog/z80`). MIT license.
- Go workspace with two modules: the **library module** (repo root,
  stdlib-only **forever** — verified with `go mod graph`) and the **cmd
  module** (`cmd/`, own `go.mod`, holds `zrun` now and the bubbletea
  `zstep` in Phase 4).
- Go 1.26 toolchain in the devcontainer; `go.mod` declares `go 1.24`
  (minimum supported).
- Assembler: **pasmo** (Debian package) is the default for all examples
  and workflows. sjasmplus 1.23.1 is also installed (built from source)
  for future Galaksija work.
- Dispatch: 256-entry function tables per prefix page, hand-written
  micro-op helpers own all timing. DD/FD are a register-remap mode over
  the base table, not separate tables. No code generation.
- SingleStepTests data (~277 MB packed) is downloaded by
  `.devcontainer/post_create.sh` at a pinned commit into
  `z80/testdata/sst/` (gitignored); tests skip with an explicit message
  when it is absent.
- Distribution: the Go module is consumed straight from GitHub
  (proxy.golang.org caches it; pkg.go.dev documents it) — semver git
  tags are the releases. A JS/WASM binding is published to npm as
  `gozilog` from `bindings/npm`: TinyGo-compiled (331 KB vs 2.8 MB),
  gated on a 2000-case differential test against the reference Go
  build plus functional tests, published by CI with provenance on
  version tags.
- WebAssembly is an officially supported target (both `js/wasm` and
  `wasip1/wasm`; TinyGo not gated). The library needed zero changes —
  the guarantee is enforced by `tools/check-wasm.sh`: build checks, the
  full `-short` suite under wasmtime and Node, a golden-dump
  determinism diff of the wasm-compiled zrun, and a headless Node check
  of the browser demo (`examples/wasm/`, its own module). wasmtime is
  pinned in the Dockerfile; `wasm_exec.js` is copied at build time from
  the installed GOROOT (it must match the compiling Go version, so it
  is never committed). `cmd/zstep` is native-only (TTY).

## Public API

Defined in `z80/bus.go`, `z80/cpu.go`, `z80/state.go` (godoc is the
authoritative reference). Summary:

- `Bus` (required): `MemRead`, `MemWrite`, `IORead`, `IOWrite`. Pure
  data, zero timing responsibility. Full 16-bit port addresses.
- `Ticker` (optional): `Tick(addr uint16, data int16, pins Pins) int`,
  called **once per T-state** with the address bus, data bus
  (`DataNone` = not driven) and control pins (`MREQ IORQ RD WR M1 RFSH
  HALTP`). The return value inserts that many wait T-states before the
  CPU proceeds — the contention mechanism. Detected once in `New`.
- `IntAcker` (optional): `IntAck() byte` supplies data-bus bytes during
  INT acknowledge (IM2 vector; IM0 instruction bytes). Default: 0xFF
  (floating bus ⇒ RST 38h).
- `CPU`: `New(bus)`, `Step() int`, `Run(tstates int) int`,
  `SetINT(bool)` (level-triggered), `NMI()` (edge-latched), `Reset()`,
  `Tstates() uint64`, `Halted() bool`, `State()/SetState(State)`.
- `State`: every register plus hidden state — WZ (MEMPTR), IFF1/IFF2,
  IM, R, Halted, EIPending (the SST `ei` latch), Q (X/Y flag latch for
  SCF/CCF), P (LD A,I/R erratum latch).

Determinism: no wall clock, no goroutines, no map iteration on any
execution path. Identical initial state + identical bus behavior ⇒
byte-identical results. All CPU methods are single-goroutine.

## Timing model

The CPU owns the datasheet T-state schedule; machines observe it through
`Ticker` and stretch it via wait states. Every T-state of every M-cycle
is emitted exactly once. The schedule lives **only** in the micro-op
helpers in `z80/cpu.go` (`fetchOpcode`, `read8`, `write8`, `ioRead`,
`ioWrite`, `internal`, `refresh`, plus the interrupt-acceptance
sequences) — opcode handlers never tick directly.

Per-M-cycle schedules, verified T-state-exact against the
SingleStepTests cycle traces (whose convention we adopt: control pins
are reported on the T-state where the transaction is sampled; read data
appears on the following T-state; write data coincides with WR; the
fetched opcode rides the refresh T3):

| M-cycle | T-states | Schedule (addr · data · pins per T) |
|---|---|---|
| Opcode fetch (M1) | 4 | T1 @PC `M1` · T2 @PC `M1\|MREQ\|RD` (waits) · T3 @I<<8\|R +opcode `RFSH` · T4 @I<<8\|R `RFSH` |
| Memory read | 3 | T1 @addr · T2 `MREQ\|RD` (waits) · T3 +data |
| Memory write | 3 | T1 @addr · T2 +data `MREQ\|WR` (waits) · T3 |
| I/O read | 4 | T1, T2 @port · TW `IORQ\|RD` (waits) · T3 +data |
| I/O write | 4 | T1, T2 @port · TW +data `IORQ\|WR` (waits) · T4 |
| Internal | n | last M1 refresh address (or the just-accessed pointer in block-op repeats), no pins, no data |
| INT ack | 6 | T1, TW1 `M1` · TW2 `M1\|IORQ` (waits) · T2 +ack data `M1` · T3, T4 `RFSH` |
| NMI ack | 5 | normal-shape fetch (4, data discarded, PC kept) + 1 internal |
| HALT | 4/cycle | M1 fetch at PC, data discarded, `HALTP` set, R increments |

The refresh address always carries the pre-increment R. Interrupt
totals: NMI 11T, IM1 13T, IM2 19T, IM0 = 6T ack + the injected
instruction's own cycles beyond its opcode fetch.

Facts already verified against SST traces:

- The refresh address carries the **pre-increment** R value.
- HALT leaves PC past the instruction; the halted CPU keeps fetching
  (and discarding) the byte at PC.
- SST cycle records are `[addr|null, data|null, "rwmi"]` — RD/WR/MREQ/
  IORQ only; M1/RFSH are not recorded there and are masked in the
  harness comparison.

R register: incremented once per M1 (opcode fetch, halt cycle, interrupt
acknowledge; prefixed opcodes will increment once per prefix fetch in
Phase 2). Bit 7 is preserved; only bits 0–6 count.

MEMPTR: implemented per the Boo-boo document (English translation:
https://raw.githubusercontent.com/floooh/emu-info/master/z80/memptr_eng.txt),
asserted by SST for every case. Special RESET reference (Phase 3, last):
http://www.primrosebank.net/computers/z80/z80_special_reset.htm

Interrupts: `SetINT` is level-triggered and sampled at instruction
boundaries in `Step`; EI defers acceptance by one instruction
(`EIPending`). `NMI()` latches an edge, accepted regardless of IFF1
(11 T-states, IFF1 cleared, IFF2 preserved, jump to 0x0066). IM1 = 13T,
IM2 = 19T. IM0 executes the acknowledged byte; multi-byte IM0
instructions are a Phase 3 item (single-byte, i.e. RST n, works — which
is what real vectorless hardware produces).

## Testing and acceptance gates

1. **SingleStepTests** (`z80/sst_test.go`) — primary gate. State-level
   compare of the full `State` plus RAM plus total T-state count, for
   every implemented opcode (`implementedBase`); unimplemented opcodes
   are listed explicitly in the test log, never silently skipped.
   Per-T-state trace comparison is implemented behind `Z80_SST_CYCLES=1`
   and becomes a hard assertion in Phase 3.
2. **ZEXDOC/ZEXALL** (Phase 3): binaries from
   https://github.com/agn453/ZEXALL (GPL-licensed test fixtures —
   downloaded, not committed), run under a CP/M stub trapping PC=0x0005
   (BDOS output) and 0x0000 (warm boot). ~46.7 billion T-states per run:
   gate behind `-short`/build tag.
3. `go test ./...` green in both modules, `-race` where applicable.
4. Determinism: `TestHelloGolden` (cmd module) assembles
   `examples/hello_from_z80.asm` with pasmo, runs it twice, and requires
   both dumps byte-identical to `examples/hello_from_z80.golden`.

Never weaken, skip, or special-case tests to make them pass. When a test
fails, the emulator is wrong until proven otherwise; genuine reference
discrepancies go in the AGENTS.md discrepancy log.

## Phase status

- **Phase 1 — Foundation: complete.** Devcontainer, workspace, public
  API, timing model, SST harness, zrun + example + golden dump, docs.
- **Phase 2 — Full instruction set: complete.** All 1604 SST files pass
  at state level (every register incl. WZ/Q/P, RAM, port transactions,
  T-state totals): full unprefixed set, CB page (incl. SLL), ED page
  (incl. undocumented IN (C)/OUT (C),0, NEG/RETN mirrors, block-op
  repeat flags), DD/FD remapping incl. DDCB/FDCB with the register-copy
  behavior. Undocumented X/Y flags and MEMPTR are already correct
  everywhere at state level, since SST asserts F and WZ per case.
- **Phase 3 — Cycle accuracy & esoterica: complete.** All 1604 SST
  files pass the always-on per-T-state cycle-trace assertion (address
  bus, data bus, RD/WR/MREQ/IORQ at every T-state). ZEXDOC and ZEXALL
  run to completion with every CRC OK (~46.7B T-states each, ~100 s
  wall clock, skipped under `go test -short`). IM0 executes multi-byte
  injected instructions with all bytes from IntAck. SpecialReset()
  implements the Brewer special-RESET semantics.
- **Phase 4 — Polish & showcase: complete.** `cmd/zstep` bubbletea TUI
  stepper (headless model tests + pty smoke test), F5 launch configs
  for zrun and zstep, README quickstart, fresh-clone gate: clone →
  image build → post-create data download → all tests green → example
  golden dump reproduced → showcase builds.

## References

- Primary behavior reference: https://github.com/redcode/Z80 (per-M-cycle
  callback C library; we chose finer per-T-state granularity for
  Galaksija's sake). Secondary: https://github.com/ha1tch/zen80,
  https://github.com/remogatto/z80 (FUSE lineage, GPL — **ideas only, no
  code**). All reference code carries incompatible licenses and different
  architectures; everything here is written from documentation and test
  data.
- SingleStepTests: https://github.com/SingleStepTests/z80, pinned in
  `.devcontainer/post_create.sh`.
