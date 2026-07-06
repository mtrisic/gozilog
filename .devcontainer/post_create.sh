#!/usr/bin/env bash
# Runs once after the devcontainer is created.
set -euo pipefail

WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# SingleStepTests/z80 commit the test suite is pinned to. Update deliberately;
# the JSON schema and test contents are part of this project's acceptance gate.
SST_SHA="ebe1875d48f374bcfd4b505d8eb8ee751568b5f7"
SST_DIR="$WORKSPACE/z80/testdata/sst"

echo "==> Configuring persistent shell history on /commandhistory"
sudo chown -R "$(id -un):$(id -gn)" /commandhistory
touch /commandhistory/.zsh_history /commandhistory/.bash_history
for rc in "$HOME/.zshrc" "$HOME/.bashrc"; do
    if [ -f "$rc" ] && ! grep -q "commandhistory" "$rc"; then
        {
            echo ''
            echo '# Persist shell history on the devcontainer volume'
            if [[ "$rc" == *zshrc ]]; then
                echo 'export HISTFILE=/commandhistory/.zsh_history'
                echo 'setopt INC_APPEND_HISTORY'
            else
                echo 'export HISTFILE=/commandhistory/.bash_history'
            fi
            echo 'export HISTSIZE=100000'
            echo 'export SAVEHIST=100000'
        } >> "$rc"
    fi
done

if [ -d "$SST_DIR/v1" ]; then
    echo "==> SingleStepTests already present in $SST_DIR (skipping download)"
else
    echo "==> Downloading SingleStepTests/z80 @ ${SST_SHA:0:12} (~280 MB, one time)"
    mkdir -p "$SST_DIR"
    tmp="$(mktemp -d)"
    curl -fsSL "https://codeload.github.com/SingleStepTests/z80/tar.gz/$SST_SHA" \
        | tar -xz -C "$tmp"
    mv "$tmp/z80-$SST_SHA/v1" "$SST_DIR/v1"
    echo "$SST_SHA" > "$SST_DIR/COMMIT"
    rm -rf "$tmp"
    echo "==> $(ls "$SST_DIR/v1" | wc -l) test files installed"
fi

# ZEXDOC/ZEXALL exerciser binaries (GPL-licensed test fixtures — kept
# out of the repo, fetched at a pinned commit).
ZEX_SHA="319f7f958a15791f91d4d92a84bfdbadb7908c2a"
ZEX_DIR="$WORKSPACE/z80/testdata/zex"
if [ -f "$ZEX_DIR/zexall.com" ]; then
    echo "==> ZEX binaries already present in $ZEX_DIR (skipping download)"
else
    echo "==> Downloading ZEXDOC/ZEXALL @ ${ZEX_SHA:0:12}"
    mkdir -p "$ZEX_DIR"
    for f in zexall.com zexdoc.com; do
        curl -fsSL -o "$ZEX_DIR/$f" \
            "https://raw.githubusercontent.com/agn453/ZEXALL/$ZEX_SHA/$f"
    done
    echo "$ZEX_SHA" > "$ZEX_DIR/COMMIT"
fi

echo "==> Syncing Go workspace"
cd "$WORKSPACE" && go work sync

echo "==> Toolchain versions"
go version
pasmo -v 2>&1 | head -1 || true
sjasmplus 2>&1 | grep -m1 SjASMPlus || true

echo "==> Done. Try: go test ./..."
