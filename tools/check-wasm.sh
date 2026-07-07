#!/usr/bin/env bash
# WebAssembly gate for gozilog. Run inside the devcontainer:
#
#   bash tools/check-wasm.sh          # full: builds + test suites + determinism
#   bash tools/check-wasm.sh quick    # build checks only (~seconds)
#
# Verifies that the officially supported WASM targets stay first-class:
#   - the z80 library and cmd/zrun build for js/wasm and wasip1/wasm
#     (cmd/zstep is deliberately excluded: bubbletea needs a real TTY)
#   - the full library test suite (SingleStepTests incl. per-T-state
#     cycle traces; ZEX skipped via -short) passes under wasmtime
#     (wasip1) and Node (js)
#   - zrun compiled to wasip1 and executed by wasmtime reproduces the
#     committed golden RAM dump byte-for-byte (cross-architecture
#     determinism: wasm32 vs native)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

WASM_EXEC_DIR="$(go env GOROOT)/lib/wasm"

echo "==> Build checks"
GOOS=js     GOARCH=wasm go build ./z80/...          && echo "    z80 library  js/wasm     OK"
GOOS=wasip1 GOARCH=wasm go build ./z80/...          && echo "    z80 library  wasip1/wasm OK"
GOOS=js     GOARCH=wasm go build -o /dev/null ./cmd/zrun && echo "    cmd/zrun     js/wasm     OK"
GOOS=wasip1 GOARCH=wasm go build -o /dev/null ./cmd/zrun && echo "    cmd/zrun     wasip1/wasm OK"

if [ "${1:-}" = "quick" ]; then
    echo "==> quick mode: skipping WASM test-suite runs"
    exit 0
fi

echo "==> Library tests under wasip1 (wasmtime; ~2 min)"
GOOS=wasip1 GOARCH=wasm go test -short -count=1 \
    -exec "$WASM_EXEC_DIR/go_wasip1_wasm_exec" ./z80/...

echo "==> Library tests under js (Node; ~2 min)"
GOOS=js GOARCH=wasm go test -short -count=1 \
    -exec "$WASM_EXEC_DIR/go_js_wasm_exec" ./z80/...

echo "==> Cross-arch determinism: zrun.wasm under wasmtime vs golden dump"
mkdir -p build
pasmo examples/hello_from_z80.asm build/hello_from_z80.bin
GOOS=wasip1 GOARCH=wasm go build -o build/zrun.wasm ./cmd/zrun
# --env PWD is how Go's wasip1 runtime learns the working directory;
# without it relative guest paths resolve against / (cf. GOROOT's
# go_wasip1_wasm_exec, which does the same).
wasmtime run --dir=/ --env PWD="$PWD" build/zrun.wasm -org 0x8000 build/hello_from_z80.bin 2>/dev/null \
    | diff examples/hello_from_z80.golden -
echo "    wasm RAM dump identical to committed golden"

echo "==> Browser demo: build + headless API check under Node"
(cd examples/wasm && bash build.sh >/dev/null && node check.js)

echo "==> WASM gate complete"
