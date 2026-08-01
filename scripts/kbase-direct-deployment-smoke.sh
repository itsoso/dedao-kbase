#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
README="${ROOT}/README.md"
BUILD_WORKFLOW="${ROOT}/.github/workflows/kbase-build-gates.yml"
RELEASE_WORKFLOW="${ROOT}/.github/workflows/kbase-release-gates.yml"

fail() {
  printf 'kbase-direct-deployment-smoke: %s\n' "$*" >&2
  exit 1
}

shopt -s nullglob
release_kit_files=("${ROOT}"/deploy/kbase/*)
[[ "${#release_kit_files[@]}" -eq 0 ]] ||
  fail "retired release-kit file remains: ${release_kit_files[0]#"$ROOT"/}"

[[ ! -e "$RELEASE_WORKFLOW" ]] ||
  fail "retired release workflow remains"
[[ -f "$BUILD_WORKFLOW" ]] ||
  fail "normal build workflow is missing"
grep -Fq 'name: KBase Build Gates' "$BUILD_WORKFLOW" ||
  fail "normal build workflow has the wrong identity"

for forbidden in \
  'MANIFEST.sig' \
  'release-signature.sh' \
  'prepare-release.sh' \
  'install-release.sh'
do
  if grep -Fq "$forbidden" "$README" "$BUILD_WORKFLOW"; then
    fail "active deployment contract still references ${forbidden}"
  fi
done

for required in \
  '### KBase direct deployment' \
  'git archive' \
  'sha256' \
  'KBASE_REVISION' \
  'runuser --user' \
  'go test ./...' \
  'go build' \
  'KBASE_BACKUP_DIR' \
  'rollback_direct_deployment()' \
  'trap rollback_direct_deployment ERR' \
  'trap - ERR' \
  'systemctl restart' \
  'KBASE_LOOPBACK_HEALTH_URL' \
  'KBASE_PUBLIC_HEALTH_URL'
do
  grep -Fq "$required" "$README" ||
    fail "README is missing direct-deployment contract: ${required}"
done

for required in \
  'go mod verify' \
  'npm run build' \
  'go vet ./...' \
  'go test ./...' \
  'browser-session-proxy-smoke.sh' \
  'privacy-smoke.sh' \
  'system-map-smoke.sh'
do
  grep -Fq "$required" "$BUILD_WORKFLOW" ||
    fail "normal build workflow is missing gate: ${required}"
done

if grep -Eq \
  'MANIFEST\.sig|release-signature|assemble-release|prepare-release|install-release' \
  "$BUILD_WORKFLOW"
then
  fail "normal build workflow still contains release-kit behavior"
fi

printf 'kbase direct deployment smoke passed\n'
