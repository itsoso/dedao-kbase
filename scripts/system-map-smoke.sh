#!/usr/bin/env bash
set -euo pipefail

tmp_file="$(mktemp)"
trap 'rm -f "$tmp_file"' EXIT

go run ./cmd/system-map --root . --out "$tmp_file"
if ! cmp -s "$tmp_file" docs/_generated/system-map.json; then
  echo "docs/_generated/system-map.json is stale. Run: go run ./cmd/system-map --root . --out docs/_generated/system-map.json" >&2
  diff -u docs/_generated/system-map.json "$tmp_file" >&2 || true
  exit 1
fi

if grep -E '(/Users/|/private/|KBASE_AUTH_TOKEN|KBASE_SOURCE_AGENT_TOKEN|Bearer )' docs/_generated/system-map.json >/dev/null; then
  echo "generated system map contains a private path or secret-like token" >&2
  exit 1
fi
