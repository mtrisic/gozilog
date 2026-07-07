#!/usr/bin/env bash
# Build gozilog.wasm + the matching wasm_exec.js for the npm package.
#
#   bash build.sh            # TinyGo (small binary — the shipped default)
#   TOOLCHAIN=go bash build.sh   # standard Go toolchain (large, reference build)
#
# The wasm_exec.js glue is toolchain-specific and MUST come from the
# toolchain that built the binary; the script pairs them automatically.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

case "${TOOLCHAIN:-tinygo}" in
tinygo)
    tinygo build -o gozilog.wasm -target wasm -no-debug ./wasm
    cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" .
    ;;
go)
    GOOS=js GOARCH=wasm go build -o gozilog.wasm ./wasm
    cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
    ;;
*)
    echo "unknown TOOLCHAIN '${TOOLCHAIN}' (want tinygo or go)" >&2
    exit 1
    ;;
esac

echo "built gozilog.wasm with ${TOOLCHAIN:-tinygo}: $(wc -c < gozilog.wasm | tr -d ' ') bytes"
