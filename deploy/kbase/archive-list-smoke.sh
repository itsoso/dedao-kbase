#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PARSER="${SCRIPT_DIR}/archive-list.mjs"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

expect_failure_contains() {
  description="$1"
  expected="$2"
  shift 2

  stderr_file="${TMP_ROOT}/failure.stderr"
  if "$@" >"${TMP_ROOT}/failure.stdout" 2>"$stderr_file"; then
    fail "${description}: command unexpectedly succeeded"
  fi
  if ! grep -F "$expected" "$stderr_file" >/dev/null; then
    printf '%s\n' "---- ${description} stderr ----" >&2
    cat "$stderr_file" >&2
    fail "${description}: expected stderr to contain: ${expected}"
  fi
}

[[ -f "$PARSER" ]] || fail "archive parser is missing: ${PARSER}"
command -v node >/dev/null 2>&1 || fail "node is required"
command -v gzip >/dev/null 2>&1 || fail "gzip is required"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kbase-archive-list.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

ARCHIVE="${TMP_ROOT}/fixture.tar.gz"
CORRUPT_ARCHIVE="${TMP_ROOT}/corrupt.tar.gz"
FAKE_TAR="${TMP_ROOT}/fake-tar"
TAR_COUNT="${TMP_ROOT}/tar-count"
TIMEOUT_MARKER="${TMP_ROOT}/timeout-marker"
GZIP_BIN="$(command -v gzip)"

printf 'compressed fixture\n' | "$GZIP_BIN" -c >"$ARCHIVE"
printf 'not a gzip stream\n' >"$CORRUPT_ARCHIVE"

cat >"$FAKE_TAR" <<'FAKE_TAR'
#!/usr/bin/env bash
set -euo pipefail

expected=(
  "--numeric-owner"
  "--full-time"
  "--quoting-style=escape"
  "-tvf"
  "-"
)
actual=("$@")
if [[ "$#" -ne "${#expected[@]}" ]]; then
  printf 'unexpected tar argument count: %s\n' "$#" >&2
  exit 91
fi
for index in "${!expected[@]}"; do
  if [[ "${actual[$index]}" != "${expected[$index]}" ]]; then
    printf 'unexpected tar argument at %s\n' "$index" >&2
    exit 92
  fi
done

printf 'call\n' >>"${FAKE_TAR_COUNT:?}"
cat >/dev/null

case "${FAKE_TAR_MODE:-success}" in
  success)
    cat <<'LISTING'
-rw-r--r-- 0/0 5 2026-07-29 12:34:56.000000000 +0000 bundle/file with spaces.txt
drwxr-xr-x 0/0 0 2026-07-29 12:34:56.000000000 +0000 bundle/directory/
lrwxrwxrwx 0/0 0 2026-07-29 12:34:56.000000000 +0000 bundle/link -> file with spaces.txt
hrw-r--r-- 0/0 0 2026-07-29 12:34:56.000000000 +0000 bundle/hard-link link to bundle/file with spaces.txt
LISTING
    ;;
  success-no-timezone)
    cat <<'LISTING'
drwxrwxr-x 0/0 0 2026-07-30 09:33:21 bundle/
lrwxrwxrwx 0/0 0 2026-07-30 09:33:21 bundle/link -> /var/tmp/external
LISTING
    ;;
  malformed)
    printf 'this is not a GNU tar verbose member\n'
    ;;
  control)
    printf '%b\n' '-rw-r--r-- 0/0 1 2026-07-29 12:34:56 +0000 bundle/bad	name'
    ;;
  escaped-control)
    printf '%s\n' '-rw-r--r-- 0/0 1 2026-07-29 12:34:56 +0000 bundle/bad\nname'
    ;;
  stdout-limit)
    printf '%05000d\n' 0
    ;;
  stderr-limit)
    printf '%05000d\n' 0 >&2
    printf '%s\n' '-rw-r--r-- 0/0 1 2026-07-29 12:34:56 +0000 bundle/file'
    ;;
  timeout)
    sleep 1
    printf 'completed\n' >"${FAKE_TAR_TIMEOUT_MARKER:?}"
    ;;
  failure)
    printf 'tar fixture failed\n' >&2
    exit 7
    ;;
  *)
    printf 'unknown fake tar mode\n' >&2
    exit 93
    ;;
esac
FAKE_TAR
chmod +x "$FAKE_TAR"

run_parser() {
  FAKE_TAR_COUNT="$TAR_COUNT" \
    FAKE_TAR_TIMEOUT_MARKER="$TIMEOUT_MARKER" \
    node "$PARSER" \
      --archive "$ARCHIVE" \
      --gzip-bin "$GZIP_BIN" \
      --tar-bin "$FAKE_TAR" \
      --timeout-ms 2000 \
      --stdout-limit-bytes 4096 \
      --stderr-limit-bytes 4096
}

FAKE_TAR_MODE=success run_parser >"${TMP_ROOT}/members.json"
node - "${TMP_ROOT}/members.json" <<'NODE'
const fs = require("fs");

const document = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const expected = {
  members: [
    { path: "bundle/file with spaces.txt", type: "file", size: 5 },
    { path: "bundle/directory/", type: "directory", size: 0 },
    { path: "bundle/link", type: "symlink", size: 0 },
    { path: "bundle/hard-link", type: "hardlink", size: 0 },
  ],
};
if (JSON.stringify(document) !== JSON.stringify(expected)) {
  throw new Error(`unexpected member document: ${JSON.stringify(document)}`);
}
NODE

[[ "$(wc -l <"$TAR_COUNT" | tr -d ' ')" == "1" ]] ||
  fail "the tar listing command must run exactly once"

FAKE_TAR_MODE=success-no-timezone run_parser >"${TMP_ROOT}/members-no-timezone.json"
node - "${TMP_ROOT}/members-no-timezone.json" <<'NODE'
const fs = require("fs");

const document = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const expected = {
  members: [
    { path: "bundle/", type: "directory", size: 0 },
    { path: "bundle/link", type: "symlink", size: 0 },
  ],
};
if (JSON.stringify(document) !== JSON.stringify(expected)) {
  throw new Error(`unexpected member document: ${JSON.stringify(document)}`);
}
NODE

FAKE_TAR_MODE=malformed \
  expect_failure_contains \
    "malformed listing" \
    "cannot parse tar verbose output" \
    run_parser

FAKE_TAR_MODE=control \
  expect_failure_contains \
    "raw control character" \
    "control character" \
    run_parser

FAKE_TAR_MODE=escaped-control \
  expect_failure_contains \
    "escaped control character" \
    "control character escape" \
    run_parser

FAKE_TAR_MODE=stdout-limit \
  expect_failure_contains \
    "stdout byte limit" \
    "stdout byte limit exceeded" \
    run_parser

FAKE_TAR_MODE=stderr-limit \
  expect_failure_contains \
    "stderr byte limit" \
    "stderr byte limit exceeded" \
    run_parser

FAKE_TAR_MODE=failure \
  expect_failure_contains \
    "tar failure" \
    "tar exited with status 7" \
    run_parser

expect_failure_contains \
  "gzip spawn failure" \
  "failed to start gzip" \
  node "$PARSER" \
    --archive "$ARCHIVE" \
    --gzip-bin "${TMP_ROOT}/missing-gzip" \
    --tar-bin "$FAKE_TAR" \
    --timeout-ms 2000 \
    --stdout-limit-bytes 4096 \
    --stderr-limit-bytes 4096

expect_failure_contains \
  "tar spawn failure" \
  "failed to start tar" \
  node "$PARSER" \
    --archive "$ARCHIVE" \
    --gzip-bin "$GZIP_BIN" \
    --tar-bin "${TMP_ROOT}/missing-tar" \
    --timeout-ms 2000 \
    --stdout-limit-bytes 4096 \
    --stderr-limit-bytes 4096

expect_failure_contains \
  "gzip process failure" \
  "gzip exited with status" \
  node "$PARSER" \
    --archive "$CORRUPT_ARCHIVE" \
    --gzip-bin "$GZIP_BIN" \
    --tar-bin "$FAKE_TAR" \
    --timeout-ms 2000 \
    --stdout-limit-bytes 4096 \
    --stderr-limit-bytes 4096

FAKE_TAR_MODE=timeout \
FAKE_TAR_COUNT="$TAR_COUNT" \
FAKE_TAR_TIMEOUT_MARKER="$TIMEOUT_MARKER" \
  expect_failure_contains \
    "listing timeout" \
    "timed out after 100 ms" \
    node "$PARSER" \
      --archive "$ARCHIVE" \
      --gzip-bin "$GZIP_BIN" \
      --tar-bin "$FAKE_TAR" \
      --timeout-ms 100 \
      --stdout-limit-bytes 4096 \
      --stderr-limit-bytes 4096

sleep 1.1
[[ ! -e "$TIMEOUT_MARKER" ]] ||
  fail "timed-out tar process was not cleaned up"

printf 'PASS: archive listing is bounded, single-pass, and strictly parsed\n'
