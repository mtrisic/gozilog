# AGENTS.md — working on gozilog

Guidance for any AI agent or human working on this repository. Read
`SPEC.md` alongside this file: it holds the design, the timing model,
the decisions log and the testing strategy. This file covers what the
project is and how to work with it.

## What this is

gozilog is a cycle-accurate Zilog Z80 CPU emulator library in Go —
machine-agnostic and dependency-free. It implements every documented
and undocumented opcode, MEMPTR (WZ), the undocumented X/Y flags, the
Q and P latches, accurate R register semantics, interrupt modes 0/1/2
and special RESET. An embedding machine supplies memory and I/O through
the `z80.Bus` interface and can observe every T-state of bus activity
(address, data, control pins) through the optional `z80.Ticker` —
precise enough to build machines where the CPU participates in video
generation or where memory is contended.

Correctness is enforced by test suites, not by inspection: all 1604
SingleStepTests files (~1.6M cases) pass with per-T-state cycle-trace
assertions, and ZEXDOC/ZEXALL report all CRCs OK.

## Repo structure

Two Go modules tied by a `go.work` workspace:

| Path | What it is |
|---|---|
| `go.mod` (root) | **the library module** `github.com/mtrisic/gozilog` — stdlib only, forever |
| `z80/` | the library: `package z80` |
| `cmd/go.mod` | separate module for executables; carries the third-party deps |
| `cmd/zrun` | headless runner: load a .bin, run to HALT, deterministic RAM dump |
| `cmd/zstep` | bubbletea TUI stepper: registers + memory view, step/run/run-to-HALT |
| `examples/` | `hello_from_z80.asm` (pasmo) + committed golden RAM dump |
| `examples/wasm/` | browser demo module (syscall/js): the emulator compiled to WebAssembly with a step/run web UI |
| `bindings/npm/` | the `gozilog` npm package: instance-based WASM binding (TinyGo build), ESM loader, TS types, tests |
| `tools/check-wasm.sh` | the WASM gate: builds + test suites under wasmtime/Node + determinism + demo check |
| `.devcontainer/` | the single supported dev environment (Go, pasmo, sjasmplus, wasmtime, Node, test-data download) |
| `.vscode/` | F5 launch configs (zrun, zstep, library tests) + build tasks |

Library source map:

| File | Contents |
|---|---|
| `z80/bus.go` | `Bus`, `Ticker`, `IntAcker`, `Pins` — the whole embedder contract |
| `z80/cpu.go` | `CPU`, `Step`/`Run`, interrupt acceptance, **micro-op helpers (all timing lives here)** |
| `z80/state.go` | `State` snapshot/restore |
| `z80/flags.go` | flag constants, szxy/parity tables, arithmetic flag helpers |
| `z80/opcodes.go` | base dispatch table, register accessors, DD/FD prefix loop |
| `z80/opcodes_cb.go` | CB page (rotates/shifts, BIT/RES/SET) |
| `z80/opcodes_ed.go` | ED page (I/O, 16-bit arithmetic, block ops incl. repeat-form flags) |
| `z80/opcodes_index.go` | DD/FD index remapping, (IX+d) resolution, DDCB/FDCB |
| `z80/sst_test.go` | SingleStepTests harness (state + RAM + ports + cycle traces) |
| `z80/zex_test.go` | ZEXDOC/ZEXALL under a CP/M stub (skipped with `-short`) |
| `z80/cpu_test.go` | interrupts, HALT, ticker/wait contract, determinism, snapshot |

Test data is downloaded by `.devcontainer/post_create.sh` at pinned
commit SHAs into `z80/testdata/` (gitignored — SST is ~280 MB, the ZEX
binaries are GPL-licensed fixtures). Tests skip with an explicit
message when the data is absent.

## Build & test

**Everything is verified inside the devcontainer.** The host is assumed
to have only Docker and VSCode; never report a build/test result from a
host toolchain.

In VSCode: open the folder, "Reopen in Container" (first build compiles
sjasmplus and downloads test data), then press F5 — the default config
assembles the example with pasmo and runs it in the emulator under the
debugger; a second config launches the `zstep` TUI.

From a terminal inside the container:

```sh
mkdir -p build
pasmo examples/hello_from_z80.asm build/hello_from_z80.bin
go run ./cmd/zrun -org 0x8000 build/hello_from_z80.bin    # run to HALT, dump RAM
go run ./cmd/zstep -org 0x8000 build/hello_from_z80.bin   # interactive stepper
go test ./... -short          # full SST suite + unit tests (~5 s)
go test ./...                 # …plus ZEXDOC/ZEXALL (~4 min)
(cd cmd && go test ./...)     # golden-dump/determinism + TUI model tests
```

Headless (no devcontainer CLI), work against a long-lived container:

```sh
docker build -f .devcontainer/Dockerfile -t gozilog-dev .
docker run -d --name gozilog-dev-ct \
    -v "$PWD":/workspace -w /workspace gozilog-dev sleep infinity
docker exec gozilog-dev-ct bash .devcontainer/post_create.sh   # one-time data download
docker exec gozilog-dev-ct bash -c "cd /workspace && go test ./... -short -race"
docker exec gozilog-dev-ct bash -c "cd /workspace && bash tools/check-wasm.sh"  # WASM gate (~5 min; 'quick' = builds only)
```

## Rules that keep the library sound

- The library module stays **stdlib-only, including test files**.
  Executables and anything with third-party deps go in the `cmd`
  module. Verify with `go mod graph` on the root module — it must list
  no third-party modules.
- Never weaken, skip, or special-case a test to make it pass. When
  SingleStepTests and a document disagree, SST wins; record the case in
  the discrepancy log below.
- Do not port or copy code from other emulators (GPL contamination —
  see SPEC.md References). Work from documentation and test data.
- **Opcode handlers never call `tick` directly.** All T-states flow
  through the micro-op helpers in `cpu.go`, so cycle accuracy is a
  property of ~10 functions. Internal T-states use
  `c.internal(addr, n)` with the trace-verified address.
- Handlers that modify F must go through `c.setF` (feeds the Q latch).
- Godoc on every exported identifier; small, focused commits.
- If SST publishes new test data, bump the pinned SHA in
  `.devcontainer/post_create.sh` deliberately and re-run everything.
- WebAssembly (`js/wasm` and `wasip1/wasm`) is an officially supported
  target: run `bash tools/check-wasm.sh` after changes that could
  affect portability. The library must keep compiling for both with no
  build tags. `cmd/zstep` is exempt (needs a TTY); `wasm_exec.js` is
  copied from GOROOT at demo build time, never committed (it must match
  the compiling Go version).
- The npm package ships a **TinyGo** build (~8x smaller); it must stay
  differentially identical to the reference Go build
  (`bindings/npm/difftest.js`, enforced by the npm release workflow).
  Releases: tag `vX.Y.Z` (Go module + npm trigger); keep
  `bindings/npm/package.json` version in lockstep with the tag. The
  `cmd` module requires a *tagged* library version (no replace
  directive) so `go install .../cmd/zrun@latest` works — bump it after
  tagging.

## Discrepancy log

Cases where references disagree; SST is the arbiter.

- Z80 datasheet describes the refresh address as carrying the
  incremented R; SST traces (opcode 00) show the **pre-increment** value
  on the bus during T3/T4. We follow SST (`fetchM1` in cpu.go).
- SST state records have no `halted` flag, so the harness masks
  `State.Halted` in comparisons; HALT semantics are covered by
  `TestHaltAndInterruptWake`.
- The memptr_eng.txt document does not mention that the repeating
  block I/O forms (INIR/INDR/OTIR/OTDR) set WZ = PC+1 when the loop is
  taken; SST asserts it and we follow SST.
- The repeat-form H/PV flag adjustment of the block I/O instructions
  is post-2020 hardware research not in classic references; the exact
  formula in `repeatIOFlags` (opcodes_ed.go) was derived from and
  verified against all 3990 repeat cases in the SST data.
- DD/FD prefix bytes clear the Q latch (observable via DD-prefixed
  SCF/CCF X/Y); classic references don't state this, SST asserts it.

## Known gaps (deliberate)

- Exotic prefix-chain semantics: exec() consumes a whole DD/FD prefix
  run inside one Step, so interrupts are implicitly deferred across the
  entire chain (real hardware defers between each prefix and the next
  byte). SST has no tests for chains; only matters for pathological
  code that idles in prefix runs while expecting interrupts.
- Interrupt sequence pin traces (INT ack, NMI) follow the documented
  totals and the SST bus conventions, but no test suite asserts them —
  SST has no interrupt tests. Timing totals are unit-tested.
- SpecialReset() is applied immediately between instructions rather
  than modeling the /RESET-sampling window of real hardware.
