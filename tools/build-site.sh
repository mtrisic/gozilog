#!/usr/bin/env bash
# Assemble the GitHub Pages site into ./site/:
#
#   /            README.md rendered to HTML (GitHub's own renderer)
#   /demo/       the WebAssembly demo (requires examples/wasm/build.sh first)
#   /assets/     images referenced by the README
#
# Used by .github/workflows/pages.yml; runs locally too (set GITHUB_TOKEN
# to avoid the anonymous rate limit of the markdown API).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

REPO_URL="https://github.com/mtrisic/gozilog"
OUT=site

test -f examples/wasm/main.wasm || {
    echo "error: demo not built — run examples/wasm/build.sh first" >&2
    exit 1
}

rm -rf "$OUT"
mkdir -p "$OUT/demo"
cp examples/wasm/index.html examples/wasm/main.wasm \
   examples/wasm/wasm_exec.js examples/wasm/hello.bin "$OUT/demo/"
cp -r assets "$OUT/assets"

# Repo-file links in the README have no target on the Pages site;
# point them at GitHub before rendering.
sed -E "s#\]\((SPEC\.md|AGENTS\.md|LICENSE)\)#]($REPO_URL/blob/master/\1)#g" \
    README.md > "$OUT/.readme.md"

auth=()
if [ -n "${GITHUB_TOKEN:-}" ]; then
    auth=(-H "Authorization: Bearer $GITHUB_TOKEN")
fi
curl -fsS "${auth[@]}" -H "Content-Type: text/plain" \
    --data-binary @"$OUT/.readme.md" \
    https://api.github.com/markdown/raw > "$OUT/.readme.html"

cat > "$OUT/index.html" <<'HEAD'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>gozilog — cycle-accurate Z80 CPU emulator library in Go</title>
<link rel="stylesheet"
      href="https://cdnjs.cloudflare.com/ajax/libs/github-markdown-css/5.8.1/github-markdown.min.css">
<style>
  body { margin: 0; }
  .markdown-body { box-sizing: border-box; min-width: 200px; max-width: 980px;
                   margin: 0 auto; padding: 45px; }
  @media (max-width: 767px) { .markdown-body { padding: 15px; } }
</style>
</head>
<body>
<article class="markdown-body">
HEAD
cat "$OUT/.readme.html" >> "$OUT/index.html"
printf '</article>\n</body>\n</html>\n' >> "$OUT/index.html"
rm "$OUT/.readme.md" "$OUT/.readme.html"

echo "site/ assembled: $(find "$OUT" -type f | wc -l | tr -d ' ') files"
