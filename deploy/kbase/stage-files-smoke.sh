#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAGER="${SCRIPT_DIR}/stage-files.mjs"
NODE_BIN="$(command -v node)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kbase-stage-files.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

fail() {
  printf 'stage files smoke: FAIL: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "${description}: command unexpectedly succeeded"
  fi
}

mkdir "${TMP_ROOT}/source" "${TMP_ROOT}/destination"
printf 'bounded input\n' >"${TMP_ROOT}/source/input.txt"

"$NODE_BIN" "$STAGER" \
  --file \
  "${TMP_ROOT}/source/input.txt" \
  "${TMP_ROOT}/destination/output.txt" \
  14 \
  0600
cmp \
  "${TMP_ROOT}/source/input.txt" \
  "${TMP_ROOT}/destination/output.txt" ||
  fail "staged bytes differ from the source"
[[ "$(
  "$NODE_BIN" - "${TMP_ROOT}/destination/output.txt" <<'NODE'
const fs = require("fs");
process.stdout.write(
  (fs.statSync(process.argv[2]).mode & 0o777).toString(8).padStart(4, "0"),
);
NODE
)" == "0600" ]] || fail "staged file mode is incorrect"

printf 'oversized\n' >"${TMP_ROOT}/source/oversized.txt"
expect_failure "oversized source rejection" \
  "$NODE_BIN" "$STAGER" \
  --file \
  "${TMP_ROOT}/source/oversized.txt" \
  "${TMP_ROOT}/destination/oversized.txt" \
  4 \
  0600
[[ ! -e "${TMP_ROOT}/destination/oversized.txt" ]] ||
  fail "oversized source left a destination file"

ln -s input.txt "${TMP_ROOT}/source/symlink.txt"
expect_failure "source symlink rejection" \
  "$NODE_BIN" "$STAGER" \
  --file \
  "${TMP_ROOT}/source/symlink.txt" \
  "${TMP_ROOT}/destination/symlink.txt" \
  64 \
  0600
[[ ! -e "${TMP_ROOT}/destination/symlink.txt" ]] ||
  fail "source symlink left a destination file"

printf 'keep\n' >"${TMP_ROOT}/destination/existing.txt"
expect_failure "existing destination rejection" \
  "$NODE_BIN" "$STAGER" \
  --file \
  "${TMP_ROOT}/source/input.txt" \
  "${TMP_ROOT}/destination/existing.txt" \
  64 \
  0600
grep -q '^keep$' "${TMP_ROOT}/destination/existing.txt" ||
  fail "existing destination was modified"

expect_failure "multi-file cleanup" \
  "$NODE_BIN" "$STAGER" \
  --file \
  "${TMP_ROOT}/source/input.txt" \
  "${TMP_ROOT}/destination/partial.txt" \
  64 \
  0600 \
  --file \
  "${TMP_ROOT}/source/oversized.txt" \
  "${TMP_ROOT}/destination/rejected.txt" \
  4 \
  0600
[[ ! -e "${TMP_ROOT}/destination/partial.txt" ]] ||
  fail "failed batch left a partial destination"
[[ ! -e "${TMP_ROOT}/destination/rejected.txt" ]] ||
  fail "failed batch left the rejected destination"

printf 'stage files smoke: PASS\n'
