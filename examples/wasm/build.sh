#!/usr/bin/env bash
# Build the browser demo: assemble the example program, copy the
# wasm_exec.js glue matching the installed Go version, and compile the
# emulator to WebAssembly. Then: go run ./serve  →  http://localhost:8080
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

pasmo ../hello_from_z80.asm hello.bin
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
GOOS=js GOARCH=wasm go build -o main.wasm .
echo "built: main.wasm ($(wc -c < main.wasm | tr -d ' ') bytes), hello.bin, wasm_exec.js"
echo "serve: go run ./serve"
