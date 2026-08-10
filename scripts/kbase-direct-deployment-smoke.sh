#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
README="${ROOT}/README.md"
BUILD_WORKFLOW="${ROOT}/.github/workflows/kbase-build-gates.yml"
RELEASE_WORKFLOW="${ROOT}/.github/workflows/kbase-release-gates.yml"
WORKER_SERVICE="${ROOT}/deploy/systemd/dedao-book-job-worker.service"
CUTOVER_SCRIPT="${ROOT}/scripts/kbase-direct-deployment-cutover.sh"
BEHAVIOR_SMOKE="${ROOT}/scripts/kbase-direct-deployment-behavior-smoke.sh"

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
[[ -x "$CUTOVER_SCRIPT" ]] ||
  fail "production cutover script is missing or not executable"
[[ -x "$BEHAVIOR_SMOKE" ]] ||
  fail "deployment behavior smoke is missing or not executable"
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
  'KBASE_SOURCE_SHA256=' \
  'KBASE_REMOTE_SOURCE_SHA256=' \
  'test "$KBASE_REMOTE_SOURCE_SHA256" = "$KBASE_SOURCE_SHA256"' \
  'test ! -e "${KBASE_REMOTE_SOURCE_DIR:?}"' \
  'sudo mkdir -m 0700 -- "${KBASE_REMOTE_SOURCE_DIR:?}"' \
  'runuser --user' \
  'go test ./...' \
  'go build' \
  'KBASE_BACKUP_DIR' \
  'scripts/kbase-direct-deployment-cutover.sh' \
  'sudo --preserve-env=KBASE_BACKUP_DIR' \
  'KBASE_SOURCE_AGENT_ID' \
  'KBASE_BOOK_JOB_WORKER_ID' \
  'book-job-worker build-info' \
  'book-job-worker check-config' \
  './cmd/book-job-worker' \
  'main.bookJobWorkerVersion=${KBASE_VERSION}' \
  'main.bookJobWorkerRevision=${KBASE_REVISION}' \
  'KBASE_LOOPBACK_HEALTH_URL' \
  'KBASE_PUBLIC_HEALTH_URL'
do
  grep -Fq "$required" "$README" ||
    fail "README is missing direct-deployment contract: ${required}"
done

if grep -Eq 'sudo install (-[^ ]+ )*-d|sudo install -d' "$README"; then
  fail "remote source directory may not be reused with install -d"
fi
if grep -Fq 'sudo --preserve-env \' "$README"; then
  fail "deployment may not preserve the complete operator environment"
fi

cutover_required="$({
  sed -n '/^for variable in \\/,/^do$/p' "$CUTOVER_SCRIPT" |
    grep -Eo 'KBASE_[A-Z0-9_]+'
} | sort -u)"
readme_allowlist="$({
  grep -Eo 'sudo --preserve-env=[^[:space:]]+' "$README" |
    head -n 1 |
    cut -d= -f2- |
    tr ',' '\n'
} | sort -u)"
readme_exports="$({
  sed -n '/DEPLOY_EXPORTS_BEGIN/,/DEPLOY_EXPORTS_END/p' "$README" |
    grep -Eo 'KBASE_[A-Z0-9_]+' || true
} | sort -u)"
[[ "$cutover_required" == "$readme_allowlist" ]] ||
  fail "README sudo allowlist differs from cutover required variables"
[[ "$cutover_required" == "$readme_exports" ]] ||
  fail "README exports differ from cutover required variables"

for required in \
  'canonicalize_cutover_path()' \
  '${KBASE_BOOK_KNOWLEDGE_ROOT:?}/book_jobs.sqlite3' \
  'KBASE_BOOK_JOBS_DB does not match the Worker database path'
do
  grep -Fq "$required" "$CUTOVER_SCRIPT" ||
    fail "cutover script is missing database path guard: ${required}"
done

for required in \
  'KBASE_LOOPBACK_READY_MAX_ATTEMPTS=30' \
  'KBASE_LOOPBACK_READY_DELAY_SECONDS=1' \
  'wait_for_kbase_readiness()' \
  'attempt < KBASE_LOOPBACK_READY_MAX_ATTEMPTS' \
  'sleep "$KBASE_LOOPBACK_READY_DELAY_SECONDS"'
do
  grep -Fq "$required" "$CUTOVER_SCRIPT" ||
    fail "cutover script is missing bounded readiness retry: ${required}"
done
test "$(grep -Ec '^[[:space:]]*wait_for_kbase_readiness$' "$CUTOVER_SCRIPT")" -eq 2 ||
  fail "bounded readiness retry must protect cutover and rollback"

for required in \
  'sudo rm -f "${KBASE_WORKER_BINARY_TARGET:?}"' \
  'sudo rm -f "${KBASE_WORKER_UNIT_TARGET:?}"' \
  'book-job-worker.absent' \
  'book_jobs.sqlite3.absent' \
  'jobs.json.absent' \
  'dedao-book-job-worker.service.absent'
do
  grep -Fq "$required" "$CUTOVER_SCRIPT" ||
    fail "cutover script is missing first-rollout recovery: ${required}"
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
  local file="$1"
  local needle="$2"
  local line
  line="$(grep -nF "$needle" "$file" | head -n 1 | cut -d: -f1)"
  [[ -n "$line" ]] || fail "deployment script is missing ordered marker: ${needle}"
  printf '%s\n' "$line"
}

trap_line="$(line_of "$CUTOVER_SCRIPT" 'trap rollback_direct_deployment ERR')"
database_guard_line="$(line_of "$CUTOVER_SCRIPT" 'KBASE_BOOK_JOBS_DB does not match the Worker database path')"
backup_line="$(line_of "$CUTOVER_SCRIPT" 'sudo install -d -o root -g root -m 0700 "${KBASE_BACKUP_DIR:?}"')"
[[ "$database_guard_line" -lt "$backup_line" ]] ||
  fail "database path guard must run before backup creation"
replace_line="$(line_of "$CUTOVER_SCRIPT" 'sudo mv "${KBASE_BINARY_CANDIDATE_TARGET:?}" "${KBASE_BINARY_TARGET:?}"')"
[[ "$trap_line" -lt "$replace_line" ]] ||
  fail "rollback trap must be installed before replacement"

stop_worker_line="$(line_of "$CUTOVER_SCRIPT" 'sudo systemctl stop "${KBASE_WORKER_SERVICE_NAME:?}"')"
stop_server_line="$(line_of "$CUTOVER_SCRIPT" 'sudo systemctl stop "${KBASE_SERVICE_NAME:?}"')"
export_legacy_line="$(line_of "$CUTOVER_SCRIPT" 'export-legacy --out "${KBASE_LEGACY_JOBS_TEMP:?}"')"
restore_server_line="$(line_of "$CUTOVER_SCRIPT" '"${KBASE_BACKUP_DIR:?}/kbase-server" \')"
restart_server_line="$(line_of "$CUTOVER_SCRIPT" 'sudo systemctl restart "${KBASE_SERVICE_NAME:?}"')"
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
  'systemd-analyze verify deploy/systemd/dedao-book-job-worker.service' \
  'bash scripts/kbase-direct-deployment-behavior-smoke.sh' \
  'KBASE_REMOTE_URL=' \
  'KBASE_SOURCE_AGENT_TOKEN=' \
  'KBASE_SOURCE_AGENT_ID=' \
  'if ! id -u dedao-kbase'
do
  grep -Fq "$required" "$BUILD_WORKFLOW" ||
    fail "normal build workflow is missing gate: ${required}"
done

if grep -Fq 'useradd --system --no-create-home dedao-kbase || true' "$BUILD_WORKFLOW"; then
  fail "CI hides service-user creation failures"
fi

if grep -Eq \
  'MANIFEST\.sig|release-signature|assemble-release|prepare-release|install-release' \
  "$BUILD_WORKFLOW"
then
  fail "normal build workflow still contains release-kit behavior"
fi

printf 'kbase direct deployment smoke passed\n'
