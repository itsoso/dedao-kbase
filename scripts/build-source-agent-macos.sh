#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
output="${SOURCE_AGENT_OUTPUT:-$root/build/bin/source-agent}"
mkdir -p "$(dirname "$output")"
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -o "$output" "$root/cmd/source-agent"
if [[ -n "${CODESIGN_IDENTITY:-}" ]]; then
  codesign --force --options runtime --sign "$CODESIGN_IDENTITY" "$output"
fi
shasum -a 256 "$output"
