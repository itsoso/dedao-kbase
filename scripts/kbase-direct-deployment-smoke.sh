#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
README="${ROOT}/README.md"
BUILD_WORKFLOW="${ROOT}/.github/workflows/kbase-build-gates.yml"
RELEASE_WORKFLOW="${ROOT}/.github/workflows/kbase-release-gates.yml"
WORKER_SERVICE="${ROOT}/deploy/systemd/dedao-book-job-worker.service"

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
[[ -f "$WORKER_SERVICE" ]] ||
  fail "book job worker systemd service is missing"
grep -Fq 'name: KBase Build Gates' "$BUILD_WORKFLOW" ||
  fail "normal build workflow has the wrong identity"

for required in \
  'Description=KBase book job worker' \
  'After=network-online.target dedao-kbase.service' \
  'Wants=dedao-kbase.service' \
  'EnvironmentFile=/etc/dedao-kbase/kbase.env' \
  'ExecStartPre=/opt/dedao-kbase/bin/book-job-worker check-config' \
  'ExecStart=/opt/dedao-kbase/bin/book-job-worker run' \
  'Restart=always' \
  'KillSignal=SIGTERM' \
  'TimeoutStopSec=45'
do
  grep -Fq "$required" "$WORKER_SERVICE" ||
    fail "book job worker service is missing: ${required}"
done

if grep -Eq '^(Requires|PartOf|BindsTo)=.*dedao-kbase\.service' "$WORKER_SERVICE"; then
  fail "book job worker must survive a KBase service restart"
fi

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
  'KBASE_WORKER_CANDIDATE_BIN' \
  'KBASE_WORKER_BINARY_TARGET' \
  'KBASE_WORKER_SERVICE_NAME' \
  'KBASE_BOOK_JOBS_DB' \
  'KBASE_LEGACY_JOBS_PATH' \
  'KBASE_WORKER_UNIT_TARGET' \
  'KBASE_SOURCE_AGENT_ID' \
  'KBASE_BOOK_JOB_WORKER_ID' \
  'book-job-worker build-info' \
  'book-job-worker check-config' \
  './cmd/book-job-worker' \
  'main.bookJobWorkerVersion=${KBASE_VERSION}' \
  'main.bookJobWorkerRevision=${KBASE_REVISION}' \
  'book_jobs.sqlite3' \
  'jobs.json' \
  'book-job-worker.present' \
  'book-job-worker.absent' \
  'book_jobs.sqlite3.present' \
  'book_jobs.sqlite3.absent' \
  'jobs.json.present' \
  'jobs.json.absent' \
  'dedao-book-job-worker.service.present' \
  'dedao-book-job-worker.service.absent' \
  'export-legacy --out' \
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
  'sudo rm -f "${KBASE_WORKER_BINARY_TARGET:?}"' \
  'sudo rm -f "${KBASE_WORKER_UNIT_TARGET:?}"' \
  'test -f "${KBASE_BACKUP_DIR:?}/book-job-worker.absent"' \
  'test -f "${KBASE_BACKUP_DIR:?}/book_jobs.sqlite3.absent"' \
  'test -f "${KBASE_BACKUP_DIR:?}/jobs.json.absent"' \
  'test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.absent"'
do
  grep -Fq "$required" "$README" ||
    fail "README is missing first-rollout recovery: ${required}"
done

for required in \
  'KBASE_SERVER_SHA256=' \
  'KBASE_WORKER_SHA256=' \
  'systemctl is-active "${KBASE_SERVICE_NAME:?}"' \
  'systemctl is-active "${KBASE_WORKER_SERVICE_NAME:?}"'
do
  grep -Fq "$required" "$README" ||
    fail "README is missing dual-service verification: ${required}"
done

line_of() {
  local needle="$1"
  local line
  line="$(grep -nF "$needle" "$README" | head -n 1 | cut -d: -f1)"
  [[ -n "$line" ]] || fail "README is missing ordered marker: ${needle}"
  printf '%s\n' "$line"
}

trap_line="$(line_of 'trap rollback_direct_deployment ERR')"
replace_line="$(line_of 'sudo mv "${KBASE_BINARY_CANDIDATE_TARGET:?}" "${KBASE_BINARY_TARGET:?}"')"
[[ "$trap_line" -lt "$replace_line" ]] ||
  fail "rollback trap must be installed before replacement"

stop_worker_line="$(line_of 'sudo systemctl stop "${KBASE_WORKER_SERVICE_NAME:?}"')"
stop_server_line="$(line_of 'sudo systemctl stop "${KBASE_SERVICE_NAME:?}"')"
export_legacy_line="$(line_of 'export-legacy --out "${KBASE_LEGACY_JOBS_TEMP:?}"')"
restore_server_line="$(line_of '"${KBASE_BACKUP_DIR:?}/kbase-server" \')"
restart_server_line="$(line_of 'sudo systemctl restart "${KBASE_SERVICE_NAME:?}"')"
[[ "$stop_worker_line" -lt "$export_legacy_line" ]] ||
  fail "rollback must stop Worker before legacy export"
[[ "$stop_worker_line" -lt "$stop_server_line" ]] ||
  fail "rollback must stop Worker before KBase"
[[ "$stop_server_line" -lt "$export_legacy_line" ]] ||
  fail "rollback must stop KBase before legacy export"
[[ "$export_legacy_line" -lt "$restore_server_line" ]] ||
  fail "rollback must export SQLite before restoring the old server"
[[ "$restore_server_line" -lt "$restart_server_line" ]] ||
  fail "rollback must restore the old server before restarting it"

for required in \
  'go mod verify' \
  'npm run build' \
  'go vet ./...' \
  'go test ./...' \
  'browser-session-proxy-smoke.sh' \
  'privacy-smoke.sh' \
  'system-map-smoke.sh' \
  'go test ./cmd/book-job-worker ./cmd/kbase-server' \
  'go build' \
  './cmd/book-job-worker' \
  'book-job-worker build-info' \
  'book-job-worker check-config' \
  'systemd-analyze verify deploy/systemd/dedao-book-job-worker.service'
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
