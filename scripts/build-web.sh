#!/usr/bin/env bash
# Builds the browser/Android client: Go compiled to js/wasm plus the HTML shell.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

out=${1:-build/web}
mkdir -p "$out"

echo "==> compiling wasm"
GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w" -o "$out/app.wasm" ./cmd/chuan

echo "==> copying loader and shell"
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
cp web/index.html web/bridge.js "$out/"

ls -lh "$out"
echo "browser build ready: $out"
