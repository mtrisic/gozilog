# gozilog development environment: Go toolchain + Z80 assemblers +
# WebAssembly runtimes.
#
# Contents:
#   - Go 1.26 (devcontainers base image, Debian trixie)
#   - pasmo (Debian package) — the default assembler for examples
#   - sjasmplus (built from a pinned release) — optional, for later
#     Galaksija work; examples and default workflows use pasmo
#   - wasmtime (pinned release) — runs the test suite under
#     GOOS=wasip1 GOARCH=wasm (Go's go_wasip1_wasm_exec default)
#   - nodejs (Debian package) — runs the test suite under
#     GOOS=js GOARCH=wasm via Go's go_js_wasm_exec
FROM mcr.microsoft.com/devcontainers/go:1.26-trixie

ARG SJASMPLUS_VERSION=1.23.1
ARG WASMTIME_VERSION=46.0.1
ARG TINYGO_VERSION=0.41.1
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        pasmo \
        nodejs \
        build-essential \
        cmake \
        curl \
        ca-certificates \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

# TinyGo (pinned release .deb) — used to build the small WASM binary
# shipped in the npm package (bindings/npm).
RUN arch="$(dpkg --print-architecture)" \
    && curl -fsSL -o /tmp/tinygo.deb \
        "https://github.com/tinygo-org/tinygo/releases/download/v${TINYGO_VERSION}/tinygo_${TINYGO_VERSION}_${arch}.deb" \
    && dpkg -i /tmp/tinygo.deb \
    && rm /tmp/tinygo.deb

# wasmtime is not packaged in Debian; install the pinned release binary.
RUN arch="$(dpkg --print-architecture)" \
    && case "$arch" in arm64) wtarch=aarch64 ;; amd64) wtarch=x86_64 ;; *) echo "unsupported arch $arch" && exit 1 ;; esac \
    && curl -fsSL -o /tmp/wasmtime.tar.xz \
        "https://github.com/bytecodealliance/wasmtime/releases/download/v${WASMTIME_VERSION}/wasmtime-v${WASMTIME_VERSION}-${wtarch}-linux.tar.xz" \
    && tar -C /tmp -xJf /tmp/wasmtime.tar.xz \
    && install -m 0755 "/tmp/wasmtime-v${WASMTIME_VERSION}-${wtarch}-linux/wasmtime" /usr/local/bin/wasmtime \
    && rm -rf /tmp/wasmtime*

# sjasmplus is not packaged in Debian; build from the pinned release tarball
# (the -src.tar.xz asset bundles the required LuaBridge/lua sources).
RUN curl -fsSL -o /tmp/sjasmplus.tar.xz \
        "https://github.com/z00m128/sjasmplus/releases/download/v${SJASMPLUS_VERSION}/sjasmplus-${SJASMPLUS_VERSION}-src.tar.xz" \
    && tar -C /tmp -xJf /tmp/sjasmplus.tar.xz \
    && make -C "/tmp/sjasmplus-${SJASMPLUS_VERSION}" clean all \
    && install -m 0755 "/tmp/sjasmplus-${SJASMPLUS_VERSION}/sjasmplus" /usr/local/bin/sjasmplus \
    && rm -rf /tmp/sjasmplus*

# Persistent shell history lives on a named volume mounted at /commandhistory
# (see devcontainer.json); post_create.sh points HISTFILE there.
RUN mkdir -p /commandhistory && chown vscode:vscode /commandhistory
