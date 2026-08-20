#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/research-agent-smoke-privacy.XXXXXX")"

cleanup() {
  rm -rf "$test_root"
}
trap cleanup EXIT INT TERM

forbidden_markers=(
  'FORBIDDEN_QUESTION_BODY_MARKER'
  'FORBIDDEN_EVIDENCE_BODY_MARKER'
  'FORBIDDEN_CHAT_IDENTITY_MARKER'
  'FORBIDDEN_TOKEN_MARKER'
  'FORBIDDEN_COOKIE_MARKER'
  'FORBIDDEN_RESPONSE_BODY_MARKER'
)
payload="$(printf '%s\n' "${forbidden_markers[@]}")"

set +e
env \
  RESEARCH_AGENT_SMOKE_FAILURE_PRIVACY_SELF_TEST=1 \
  RESEARCH_AGENT_SMOKE_FAILURE_PRIVACY_PAYLOAD="$payload" \
  bash "$repo_root/scripts/research-agent-smoke.sh" \
  >"$test_root/stdout" 2>"$test_root/stderr"
status=$?
set -e

if [[ "$status" -ne 97 ]]; then
  printf 'unexpected diagnostic self-test status: %d\n' "$status" >&2
  exit 1
fi
if [[ -s "$test_root/stdout" ]]; then
  printf '%s\n' 'diagnostic self-test wrote stdout' >&2
  exit 1
fi
for marker in "${forbidden_markers[@]}"; do
  if rg -F -q "$marker" "$test_root/stderr"; then
    printf '%s\n' 'diagnostic output leaked a forbidden marker' >&2
    exit 1
  fi
done

python3 - "$test_root/stderr" <<'PY'
import re
import sys

lines = open(sys.argv[1], encoding="utf-8").read().splitlines()
assert lines and lines[0] == "research agent smoke failed: exit_status=97"
pattern = re.compile(r"^diagnostic file=(kbase\.log|worker\.err|fixture\.log) bytes=[0-9]+ sha256=[0-9a-f]{64}$")
assert len(lines) == 4
assert all(pattern.fullmatch(line) for line in lines[1:])
assert sorted(line.split()[1] for line in lines[1:]) == [
    "file=fixture.log",
    "file=kbase.log",
    "file=worker.err",
]
PY

printf '%s\n' 'research agent smoke privacy failure test passed'
