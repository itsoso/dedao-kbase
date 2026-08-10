#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CUTOVER="${ROOT}/scripts/kbase-direct-deployment-cutover.sh"
MOCK_COMMAND="${ROOT}/scripts/testdata/kbase-direct-deployment/mock-command.sh"
TEMPORARY="$(mktemp -d)"
trap 'rm -rf "$TEMPORARY"' EXIT

fail() {
  printf 'kbase direct deployment behavior smoke: %s\n' "$*" >&2
  exit 1
}

assert_file_contains() {
  local path="$1"
  local expected="$2"
  [[ -f "$path" ]] || fail "missing file: ${path}"
  grep -Fq "$expected" "$path" || fail "${path} does not contain ${expected}"
}

assert_absent() {
  [[ ! -e "$1" ]] || fail "unexpected path remains: $1"
}

setup_case() {
  local name="$1"
  CASE_DIR="${TEMPORARY}/${name}"
  mkdir -p \
    "${CASE_DIR}/bin" \
    "${CASE_DIR}/state" \
    "${CASE_DIR}/targets/frontend-web" \
    "${CASE_DIR}/sources/frontend-web" \
    "${CASE_DIR}/knowledge" \
    "${CASE_DIR}/downloads" \
    "${CASE_DIR}/staging"
  : >"${CASE_DIR}/actions.log"
  for command in sudo systemctl runuser curl sqlite3 install mv cp; do
    ln -s "$MOCK_COMMAND" "${CASE_DIR}/bin/${command}"
  done
  cp "$MOCK_COMMAND" "${CASE_DIR}/sources/book-job-worker"
  chmod 0755 "${CASE_DIR}/sources/book-job-worker"
  printf 'new-server\n' >"${CASE_DIR}/sources/kbase-server"
  printf 'new-unit\n' >"${CASE_DIR}/sources/dedao-book-job-worker.service"
  printf 'new-web\n' >"${CASE_DIR}/sources/frontend-web/version"
  printf 'old-server\n' >"${CASE_DIR}/targets/kbase-server"
  printf 'old-web\n' >"${CASE_DIR}/targets/frontend-web/version"
  printf 'download-sentinel\n' >"${CASE_DIR}/downloads/sentinel"
  touch "${CASE_DIR}/state/dedao-kbase.service.active"
}

run_cutover() {
  env \
    PATH="${CASE_DIR}/bin:${PATH}" \
    MOCK_ACTION_LOG="${CASE_DIR}/actions.log" \
    MOCK_SYSTEMCTL_STATE="${CASE_DIR}/state" \
    KBASE_UNLISTED_SENTINEL="must-not-reach-cutover" \
    KBASE_BACKUP_DIR="${CASE_DIR}/backup" \
    KBASE_BINARY_TARGET="${CASE_DIR}/targets/kbase-server" \
    KBASE_WORKER_BINARY_TARGET="${CASE_DIR}/targets/book-job-worker" \
    KBASE_WEB_TARGET="${CASE_DIR}/targets/frontend-web" \
    KBASE_BOOK_JOBS_DB="${CASE_DIR}/knowledge/book_jobs.sqlite3" \
    KBASE_LEGACY_JOBS_PATH="${CASE_DIR}/knowledge/jobs.json" \
    KBASE_WORKER_UNIT_TARGET="${CASE_DIR}/targets/dedao-book-job-worker.service" \
    KBASE_CANDIDATE_BIN="${CASE_DIR}/sources/kbase-server" \
    KBASE_WORKER_CANDIDATE_BIN="${CASE_DIR}/sources/book-job-worker" \
    KBASE_WEB_CANDIDATE_SOURCE="${CASE_DIR}/sources/frontend-web" \
    KBASE_WORKER_UNIT_CANDIDATE_SOURCE="${CASE_DIR}/sources/dedao-book-job-worker.service" \
    KBASE_BINARY_CANDIDATE_TARGET="${CASE_DIR}/staging/kbase-server" \
    KBASE_WORKER_BINARY_CANDIDATE_TARGET="${CASE_DIR}/staging/book-job-worker" \
    KBASE_WEB_CANDIDATE_TARGET="${CASE_DIR}/staging/frontend-web" \
    KBASE_WEB_PREVIOUS_TARGET="${CASE_DIR}/staging/frontend-web.previous" \
    KBASE_FAILED_WEB_TARGET="${CASE_DIR}/staging/frontend-web.failed" \
    KBASE_WORKER_UNIT_CANDIDATE_TARGET="${CASE_DIR}/staging/dedao-book-job-worker.service" \
    KBASE_SERVER_SHA256="$(sha256sum "${CASE_DIR}/sources/kbase-server" | awk '{print $1}')" \
    KBASE_WORKER_SHA256="$(sha256sum "${CASE_DIR}/sources/book-job-worker" | awk '{print $1}')" \
    KBASE_SERVICE_NAME="dedao-kbase.service" \
    KBASE_WORKER_SERVICE_NAME="dedao-book-job-worker.service" \
    KBASE_LOOPBACK_HEALTH_URL="http://127.0.0.1:8719/health" \
    KBASE_BOOK_KNOWLEDGE_ROOT="${CASE_DIR}/knowledge" \
    KBASE_SERVICE_USER="dedao-kbase" \
    sudo --preserve-env=KBASE_BACKUP_DIR,KBASE_BINARY_TARGET,KBASE_WORKER_BINARY_TARGET,KBASE_WEB_TARGET,KBASE_BOOK_JOBS_DB,KBASE_LEGACY_JOBS_PATH,KBASE_WORKER_UNIT_TARGET,KBASE_CANDIDATE_BIN,KBASE_WORKER_CANDIDATE_BIN,KBASE_WEB_CANDIDATE_SOURCE,KBASE_WORKER_UNIT_CANDIDATE_SOURCE,KBASE_BINARY_CANDIDATE_TARGET,KBASE_WORKER_BINARY_CANDIDATE_TARGET,KBASE_WEB_CANDIDATE_TARGET,KBASE_WEB_PREVIOUS_TARGET,KBASE_FAILED_WEB_TARGET,KBASE_WORKER_UNIT_CANDIDATE_TARGET,KBASE_SERVER_SHA256,KBASE_WORKER_SHA256,KBASE_SERVICE_NAME,KBASE_WORKER_SERVICE_NAME,KBASE_LOOPBACK_HEALTH_URL,KBASE_BOOK_KNOWLEDGE_ROOT,KBASE_SERVICE_USER \
      bash "$CUTOVER"
}

[[ -x "$CUTOVER" ]] || fail "production cutover script is missing"

setup_case first-install-success
run_cutover
assert_file_contains "${CASE_DIR}/targets/kbase-server" "new-server"
assert_file_contains "${CASE_DIR}/targets/book-job-worker" "case \"\$command_name\""
assert_file_contains "${CASE_DIR}/targets/frontend-web/version" "new-web"
test -f "${CASE_DIR}/state/dedao-book-job-worker.service.enabled" || fail "Worker is not enabled"
test -f "${CASE_DIR}/state/dedao-book-job-worker.service.active" || fail "Worker is not active"
test -f "${CASE_DIR}/backup/book-job-worker.absent" || fail "first install presence was not recorded"
test -f "${CASE_DIR}/backup/dedao-book-job-worker.service.absent" || fail "first unit presence was not recorded"
assert_file_contains "${CASE_DIR}/downloads/sentinel" "download-sentinel"

setup_case upgrade-success
printf 'old-worker\n' >"${CASE_DIR}/targets/book-job-worker"
printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-book-job-worker.service"
printf 'old-sqlite\n' >"${CASE_DIR}/knowledge/book_jobs.sqlite3"
printf '{"jobs":[{"id":"old"}]}\n' >"${CASE_DIR}/knowledge/jobs.json"
touch "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
touch "${CASE_DIR}/state/dedao-book-job-worker.service.active"
run_cutover
test -f "${CASE_DIR}/backup/book-job-worker.present" || fail "upgrade Worker presence was not recorded"
test -f "${CASE_DIR}/backup/book_jobs.sqlite3.present" || fail "SQLite backup presence was not recorded"
assert_file_contains "${CASE_DIR}/backup/book_jobs.sqlite3" "old-sqlite"
test -f "${CASE_DIR}/state/dedao-book-job-worker.service.enabled" || fail "upgrade disabled Worker"
test -f "${CASE_DIR}/state/dedao-book-job-worker.service.active" || fail "upgrade stopped Worker"

setup_case first-install-rollback
touch "${CASE_DIR}/state/fail-start-dedao-book-job-worker.service-once"
if run_cutover; then
  fail "injected first-install failure unexpectedly succeeded"
fi
assert_file_contains "${CASE_DIR}/targets/kbase-server" "old-server"
assert_file_contains "${CASE_DIR}/targets/frontend-web/version" "old-web"
assert_absent "${CASE_DIR}/targets/book-job-worker"
assert_absent "${CASE_DIR}/targets/dedao-book-job-worker.service"
assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.wants-symlink"
test -f "${CASE_DIR}/state/dedao-kbase.service.active" || fail "rollback did not restart KBase"
assert_file_contains "${CASE_DIR}/knowledge/jobs.json" '"jobs":[]'
test -f "${CASE_DIR}/knowledge/book_jobs.sqlite3" || fail "rollback deleted SQLite"
assert_file_contains "${CASE_DIR}/downloads/sentinel" "download-sentinel"
export_line="$(grep -n 'book-job-worker export-legacy' "${CASE_DIR}/actions.log" | head -n 1 | cut -d: -f1)"
restore_line="$(grep -n "backup/kbase-server.*targets/kbase-server" "${CASE_DIR}/actions.log" | head -n 1 | cut -d: -f1)"
[[ -n "$export_line" && -n "$restore_line" && "$export_line" -lt "$restore_line" ]] ||
  fail "legacy export did not precede binary restore"

setup_case upgrade-rollback
printf 'old-worker\n' >"${CASE_DIR}/targets/book-job-worker"
printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-book-job-worker.service"
printf 'old-sqlite\n' >"${CASE_DIR}/knowledge/book_jobs.sqlite3"
printf '{"jobs":[{"id":"old"}]}\n' >"${CASE_DIR}/knowledge/jobs.json"
touch "${CASE_DIR}/state/fail-start-dedao-kbase.service-once"
if run_cutover; then
  fail "injected upgrade failure unexpectedly succeeded"
fi
assert_file_contains "${CASE_DIR}/targets/book-job-worker" "old-worker"
assert_file_contains "${CASE_DIR}/targets/dedao-book-job-worker.service" "old-unit"
assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.active"
test -f "${CASE_DIR}/state/dedao-kbase.service.active" || fail "upgrade rollback did not restart KBase"
assert_file_contains "${CASE_DIR}/knowledge/book_jobs.sqlite3" "old-sqlite"
assert_file_contains "${CASE_DIR}/downloads/sentinel" "download-sentinel"

setup_case enabled-active-upgrade-rollback
printf 'old-worker\n' >"${CASE_DIR}/targets/book-job-worker"
printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-book-job-worker.service"
printf 'old-sqlite\n' >"${CASE_DIR}/knowledge/book_jobs.sqlite3"
printf '{"jobs":[{"id":"old"}]}\n' >"${CASE_DIR}/knowledge/jobs.json"
touch "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
touch "${CASE_DIR}/state/dedao-book-job-worker.service.active"
touch "${CASE_DIR}/state/fail-start-dedao-kbase.service-once"
if run_cutover; then
  fail "injected enabled/active upgrade failure unexpectedly succeeded"
fi
assert_file_contains "${CASE_DIR}/targets/book-job-worker" "old-worker"
assert_file_contains "${CASE_DIR}/targets/dedao-book-job-worker.service" "old-unit"
test -f "${CASE_DIR}/state/dedao-book-job-worker.service.enabled" ||
  fail "rollback did not restore enabled state"
test -f "${CASE_DIR}/state/dedao-book-job-worker.service.active" ||
  fail "rollback did not restore active state"

verify_fault_window() {
  local mode="$1"
  local fault="$2"

  setup_case "${mode}-${fault}"
  if [[ "$mode" == upgrade ]]; then
    printf 'old-worker\n' >"${CASE_DIR}/targets/book-job-worker"
    printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-book-job-worker.service"
    printf 'old-sqlite\n' >"${CASE_DIR}/knowledge/book_jobs.sqlite3"
    printf '{"jobs":[{"id":"old"}]}\n' >"${CASE_DIR}/knowledge/jobs.json"
    touch "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
    touch "${CASE_DIR}/state/dedao-book-job-worker.service.active"
  fi
  touch "${CASE_DIR}/state/fail-${fault}-once"
  if run_cutover; then
    fail "${mode} ${fault} failure unexpectedly succeeded"
  fi

  assert_file_contains "${CASE_DIR}/targets/kbase-server" "old-server"
  assert_file_contains "${CASE_DIR}/targets/frontend-web/version" "old-web"
  assert_file_contains "${CASE_DIR}/downloads/sentinel" "download-sentinel"
  assert_file_contains "${CASE_DIR}/knowledge/jobs.json" '"jobs":[]'
  test -f "${CASE_DIR}/knowledge/book_jobs.sqlite3" || fail "${mode} ${fault} deleted SQLite"
  test -f "${CASE_DIR}/state/dedao-kbase.service.active" || fail "${mode} ${fault} left KBase inactive"
  if [[ "$mode" == upgrade ]]; then
    assert_file_contains "${CASE_DIR}/targets/book-job-worker" "old-worker"
    assert_file_contains "${CASE_DIR}/targets/dedao-book-job-worker.service" "old-unit"
    test -f "${CASE_DIR}/state/dedao-book-job-worker.service.enabled" ||
      fail "${mode} ${fault} did not restore enabled state"
    test -f "${CASE_DIR}/state/dedao-book-job-worker.service.active" ||
      fail "${mode} ${fault} did not restore active state"
  else
    assert_absent "${CASE_DIR}/targets/book-job-worker"
    assert_absent "${CASE_DIR}/targets/dedao-book-job-worker.service"
    assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
    assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.wants-symlink"
  fi
  export_line="$(grep -n 'book-job-worker export-legacy' "${CASE_DIR}/actions.log" | head -n 1 | cut -d: -f1)"
  restore_line="$(grep -n "backup/kbase-server.*targets/kbase-server" "${CASE_DIR}/actions.log" | head -n 1 | cut -d: -f1)"
  [[ -n "$export_line" && -n "$restore_line" && "$export_line" -lt "$restore_line" ]] ||
    fail "${mode} ${fault} restored before legacy export"
}

for mode in first-install upgrade; do
  for fault in server-move worker-move web-old-move web-new-move unit-move daemon-reload-after-unit; do
    verify_fault_window "$mode" "$fault"
  done
done

verify_pretrap_failure() {
  local fault="$1"
  setup_case "pretrap-${fault}"
  touch "${CASE_DIR}/state/fail-${fault}-once"
  if run_cutover; then
    fail "pre-trap ${fault} failure unexpectedly succeeded"
  fi
  assert_file_contains "${CASE_DIR}/targets/kbase-server" "old-server"
  assert_file_contains "${CASE_DIR}/targets/frontend-web/version" "old-web"
  assert_absent "${CASE_DIR}/targets/book-job-worker"
  assert_absent "${CASE_DIR}/targets/dedao-book-job-worker.service"
  test -f "${CASE_DIR}/state/dedao-kbase.service.active" || fail "pre-trap ${fault} stopped KBase"
}

verify_pretrap_failure stage-server-install
verify_pretrap_failure stage-worker-install
verify_pretrap_failure stage-web-copy

setup_case first-install-stale-enable-link
touch "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
touch "${CASE_DIR}/state/dedao-book-job-worker.service.wants-symlink"
touch "${CASE_DIR}/state/fail-server-move-once"
if run_cutover; then
  fail "stale-enable-link failure unexpectedly succeeded"
fi
assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.enabled"
assert_absent "${CASE_DIR}/state/dedao-book-job-worker.service.wants-symlink"
test -f "${CASE_DIR}/state/dedao-kbase.service.active" || fail "stale enable cleanup left KBase inactive"

printf 'kbase direct deployment behavior smoke passed\n'
