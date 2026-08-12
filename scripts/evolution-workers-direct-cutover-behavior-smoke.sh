#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CUTOVER="${ROOT}/scripts/evolution-workers-direct-cutover.sh"
TEMPORARY="$(mktemp -d)"
trap 'rm -rf "$TEMPORARY"' EXIT

fail() {
  printf 'evolution worker cutover behavior smoke: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  [[ -f "$1" ]] || fail "missing file: $1"
  grep -Fq "$2" "$1" || fail "$1 does not contain $2"
}

setup_case() {
  CASE_DIR="${TEMPORARY}/$1"
  mkdir -p "${CASE_DIR}/bin" "${CASE_DIR}/sources" "${CASE_DIR}/targets" "${CASE_DIR}/state"
  cat >"${CASE_DIR}/bin/sudo" <<'MOCK'
#!/usr/bin/env bash
set -Eeuo pipefail
while [[ "${1:-}" == --preserve-env=* ]]; do shift; done
exec "$@"
MOCK
  cat >"${CASE_DIR}/bin/systemctl" <<'MOCK'
#!/usr/bin/env bash
set -Eeuo pipefail
action="${1:?}"
service="${2:-}"
if [[ "$service" == --quiet ]]; then service="${3:?}"; fi
case "$action" in
  daemon-reload) printf 'daemon-reload\n' >>"${EVOLUTION_MOCK_STATE:?}/systemctl.log"; exit 0 ;;
  is-active)
    if [[ -f "${EVOLUTION_MOCK_STATE:?}/fail-is-active-${service}-always" ]]; then exit 2; fi
    if [[ -f "${EVOLUTION_MOCK_STATE:?}/${service}.active" ]]; then printf 'active\n'; exit 0; fi
    printf 'inactive\n'; exit 3
    ;;
  is-enabled)
    if [[ -f "${EVOLUTION_MOCK_STATE:?}/fail-is-enabled-${service}-always" ]]; then exit 2; fi
    if [[ -f "${EVOLUTION_MOCK_STATE:?}/${service}.enabled" ]]; then printf 'enabled\n'; exit 0; fi
    if [[ -f "${EVOLUTION_MOCK_STATE:?}/empty-is-enabled-${service}" ]]; then exit 1; fi
    printf 'disabled\n'; exit 1
    ;;
  show)
    printf '0\n'
    ;;
  stop)
    failure="${EVOLUTION_MOCK_STATE:?}/fail-stop-${service}-always"
    [[ ! -f "$failure" ]] || exit 1
    rm -f "${EVOLUTION_MOCK_STATE:?}/${service}.active"
    ;;
  start)
    failure="${EVOLUTION_MOCK_STATE:?}/fail-start-${service}-once"
    if [[ -f "$failure" ]]; then mv "$failure" "${failure}.used"; exit 1; fi
    touch "${EVOLUTION_MOCK_STATE:?}/${service}.active"
    ;;
  enable)
    touch "${EVOLUTION_MOCK_STATE:?}/${service}.enabled"
    ;;
  disable)
    rm -f "${EVOLUTION_MOCK_STATE:?}/${service}.enabled"
    ;;
  *) exit 2 ;;
esac
MOCK
  cat >"${CASE_DIR}/bin/install" <<'MOCK'
#!/usr/bin/env bash
set -Eeuo pipefail
arguments=()
  while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -o|-g) shift 2 ;;
    *) arguments+=("$1"); shift ;;
  esac
  done
if [[ "${arguments[*]}" == *knowledge-evolution-worker.candidate* ]] &&
  [[ -f "${EVOLUTION_MOCK_STATE:?}/fail-stage-knowledge-install-once" ]]; then
  mv "${EVOLUTION_MOCK_STATE:?}/fail-stage-knowledge-install-once" \
    "${EVOLUTION_MOCK_STATE:?}/fail-stage-knowledge-install-once.used"
  exit 1
fi
argument_count="${#arguments[@]}"
if ((argument_count >= 2)) &&
  [[ "${arguments[$((argument_count - 2))]}" == */backup/evaluation-worker ]] &&
  [[ "${arguments[$((argument_count - 1))]}" == */targets/evaluation-worker ]] &&
  [[ -f "${EVOLUTION_MOCK_STATE:?}/fail-rollback-evaluation-install-always" ]]; then
  touch "${EVOLUTION_MOCK_STATE:?}/rollback-install-failed"
  exit 1
fi
exec /usr/bin/install "${arguments[@]}"
MOCK
  cat >"${CASE_DIR}/bin/sleep" <<'MOCK'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ -f "${EVOLUTION_MOCK_STATE:?}/deactivate-evaluation-on-stability-wait" ]]; then
  touch "${EVOLUTION_MOCK_STATE:?}/stability-wait-observed"
  rm -f "${EVOLUTION_MOCK_STATE:?}/dedao-evolution-worker@evaluation-worker.service.active"
fi
exit 0
MOCK
  chmod +x "${CASE_DIR}/bin/sudo" "${CASE_DIR}/bin/systemctl" "${CASE_DIR}/bin/install" "${CASE_DIR}/bin/sleep"
  for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
    cat >"${CASE_DIR}/sources/${binary}" <<MOCK
#!/usr/bin/env bash
if [[ "\${1:-}" == build-info ]]; then
  printf '{"component":"${binary}","revision":"test-revision"}\\n'
  exit 0
fi
exit 2
MOCK
    chmod +x "${CASE_DIR}/sources/${binary}"
  done
  printf 'new-unit\n' >"${CASE_DIR}/sources/dedao-evolution-worker@.service"
}

run_cutover() {
  env \
    PATH="${CASE_DIR}/bin:${PATH}" \
    EVOLUTION_MOCK_STATE="${CASE_DIR}/state" \
    KBASE_EVOLUTION_BACKUP_DIR="${CASE_DIR}/backup" \
    KBASE_EVOLUTION_BINARY_DIR="${CASE_DIR}/targets" \
    KBASE_EVOLUTION_UNIT_TARGET="${CASE_DIR}/targets/dedao-evolution-worker@.service" \
    KBASE_EVOLUTION_UNIT_CANDIDATE_SOURCE="${CASE_DIR}/sources/dedao-evolution-worker@.service" \
    KBASE_EVOLUTION_UNIT_SHA256="${KBASE_EVOLUTION_UNIT_SHA256_OVERRIDE:-$(sha256sum "${CASE_DIR}/sources/dedao-evolution-worker@.service" | awk '{print $1}')}" \
    KBASE_EVOLUTION_REVISION="test-revision" \
    KBASE_AGENT_EVOLUTION_CANDIDATE_BIN="${KBASE_AGENT_EVOLUTION_CANDIDATE_OVERRIDE:-${CASE_DIR}/sources/agent-evolution-worker}" \
    KBASE_KNOWLEDGE_EVOLUTION_CANDIDATE_BIN="${CASE_DIR}/sources/knowledge-evolution-worker" \
    KBASE_EVALUATION_CANDIDATE_BIN="${CASE_DIR}/sources/evaluation-worker" \
    KBASE_AGENT_EVOLUTION_SHA256="$(sha256sum "${KBASE_AGENT_EVOLUTION_CANDIDATE_OVERRIDE:-${CASE_DIR}/sources/agent-evolution-worker}" | awk '{print $1}')" \
    KBASE_KNOWLEDGE_EVOLUTION_SHA256="$(sha256sum "${CASE_DIR}/sources/knowledge-evolution-worker" | awk '{print $1}')" \
    KBASE_EVALUATION_SHA256="${KBASE_EVALUATION_SHA256_OVERRIDE:-$(sha256sum "${CASE_DIR}/sources/evaluation-worker" | awk '{print $1}')}" \
    bash "$CUTOVER"
}

[[ -x "$CUTOVER" ]] || fail "production cutover script is missing"

setup_case first-install
run_cutover
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  assert_contains "${CASE_DIR}/targets/${binary}" "\"component\":\"${binary}\""
  service="dedao-evolution-worker@${binary}.service"
  [[ -f "${CASE_DIR}/state/${service}.enabled" ]] || fail "${service} is not enabled"
  [[ -f "${CASE_DIR}/state/${service}.active" ]] || fail "${service} is not active"
  [[ -f "${CASE_DIR}/backup/${binary}.absent" ]] || fail "${binary} first-install state was not recorded"
done
assert_contains "${CASE_DIR}/targets/dedao-evolution-worker@.service" 'new-unit'

setup_case first-install-empty-is-enabled
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  touch "${CASE_DIR}/state/empty-is-enabled-dedao-evolution-worker@${binary}.service"
done
run_cutover
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  service="dedao-evolution-worker@${binary}.service"
  [[ -f "${CASE_DIR}/state/${service}.enabled" ]] || fail "${service} is not enabled after empty first-install state"
  [[ -f "${CASE_DIR}/state/${service}.active" ]] || fail "${service} is not active after empty first-install state"
done

setup_case hash-mismatch
KBASE_EVALUATION_SHA256_OVERRIDE="$(printf 'wrong-hash' | sha256sum | awk '{print $1}')"
if run_cutover; then fail "hash mismatch unexpectedly succeeded"; fi
KBASE_EVALUATION_SHA256_OVERRIDE=""
[[ ! -e "${CASE_DIR}/backup" ]] || fail "hash mismatch changed deployment state"
[[ ! -e "${CASE_DIR}/targets/agent-evolution-worker" ]] || fail "hash mismatch installed a binary"

setup_case component-mismatch
sed 's/"component":"agent-evolution-worker"/"component":"knowledge-evolution-worker"/' \
  "${CASE_DIR}/sources/agent-evolution-worker" >"${CASE_DIR}/sources/wrong-component-worker"
chmod +x "${CASE_DIR}/sources/wrong-component-worker"
KBASE_AGENT_EVOLUTION_CANDIDATE_OVERRIDE="${CASE_DIR}/sources/wrong-component-worker"
if run_cutover; then fail "component mismatch unexpectedly succeeded"; fi
KBASE_AGENT_EVOLUTION_CANDIDATE_OVERRIDE=""
[[ ! -e "${CASE_DIR}/backup" ]] || fail "component mismatch changed deployment state"

setup_case unit-hash-mismatch
KBASE_EVOLUTION_UNIT_SHA256_OVERRIDE="$(printf 'wrong-unit' | sha256sum | awk '{print $1}')"
if run_cutover; then fail "unit hash mismatch unexpectedly succeeded"; fi
KBASE_EVOLUTION_UNIT_SHA256_OVERRIDE=""
[[ ! -e "${CASE_DIR}/backup" ]] || fail "unit hash mismatch changed deployment state"

setup_case state-query-failure
touch "${CASE_DIR}/state/fail-is-enabled-dedao-evolution-worker@agent-evolution-worker.service-always"
if run_cutover; then fail "systemctl query failure unexpectedly succeeded"; fi
[[ ! -e "${CASE_DIR}/backup" ]] || fail "query failure retained a partial backup"

setup_case staging-failure
touch "${CASE_DIR}/state/fail-stage-knowledge-install-once"
if run_cutover; then fail "staging failure unexpectedly succeeded"; fi
[[ ! -e "${CASE_DIR}/backup" ]] || fail "staging failure retained a partial backup"
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  [[ ! -e "${CASE_DIR}/targets/${binary}.candidate" ]] || fail "staging failure retained ${binary}.candidate"
done

setup_case first-install-rollback
touch "${CASE_DIR}/state/fail-start-dedao-evolution-worker@evaluation-worker.service-once"
if run_cutover; then fail "injected first-install failure unexpectedly succeeded"; fi
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  [[ ! -e "${CASE_DIR}/targets/${binary}" ]] || fail "rollback retained ${binary}"
  service="dedao-evolution-worker@${binary}.service"
  [[ ! -e "${CASE_DIR}/state/${service}.enabled" ]] || fail "rollback retained ${service} enablement"
  [[ ! -e "${CASE_DIR}/state/${service}.active" ]] || fail "rollback retained ${service} activity"
done
[[ ! -e "${CASE_DIR}/targets/dedao-evolution-worker@.service" ]] || fail "rollback retained first-install unit"

setup_case upgrade-rollback
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  printf 'old-%s\n' "$binary" >"${CASE_DIR}/targets/${binary}"
  service="dedao-evolution-worker@${binary}.service"
  touch "${CASE_DIR}/state/${service}.enabled" "${CASE_DIR}/state/${service}.active"
done
printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-evolution-worker@.service"
touch "${CASE_DIR}/state/fail-start-dedao-evolution-worker@evaluation-worker.service-once"
if run_cutover; then fail "injected upgrade failure unexpectedly succeeded"; fi
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  assert_contains "${CASE_DIR}/targets/${binary}" "old-${binary}"
  service="dedao-evolution-worker@${binary}.service"
  [[ -f "${CASE_DIR}/state/${service}.enabled" ]] || fail "rollback lost ${service} enablement"
  [[ -f "${CASE_DIR}/state/${service}.active" ]] || fail "rollback lost ${service} activity"
done
assert_contains "${CASE_DIR}/targets/dedao-evolution-worker@.service" 'old-unit'

setup_case stop-failure-before-replacement
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  printf 'old-%s\n' "$binary" >"${CASE_DIR}/targets/${binary}"
  service="dedao-evolution-worker@${binary}.service"
  touch "${CASE_DIR}/state/${service}.enabled" "${CASE_DIR}/state/${service}.active"
done
printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-evolution-worker@.service"
touch "${CASE_DIR}/state/fail-stop-dedao-evolution-worker@knowledge-evolution-worker.service-always"
if run_cutover; then fail "stop failure unexpectedly succeeded"; fi
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  assert_contains "${CASE_DIR}/targets/${binary}" "old-${binary}"
done
assert_contains "${CASE_DIR}/targets/dedao-evolution-worker@.service" 'old-unit'

setup_case unstable-after-start
touch "${CASE_DIR}/state/deactivate-evaluation-on-stability-wait"
if run_cutover; then
  [[ -f "${CASE_DIR}/state/stability-wait-observed" ]] || fail "stability wait did not execute"
  fail "unstable worker unexpectedly passed stability check"
fi
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  [[ ! -e "${CASE_DIR}/targets/${binary}" ]] || fail "unstable rollback retained ${binary}"
done
[[ ! -e "${CASE_DIR}/targets/dedao-evolution-worker@.service" ]] || fail "unstable rollback retained unit"

setup_case rollback-install-failure
for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  printf 'old-%s\n' "$binary" >"${CASE_DIR}/targets/${binary}"
  service="dedao-evolution-worker@${binary}.service"
  touch "${CASE_DIR}/state/${service}.enabled" "${CASE_DIR}/state/${service}.active"
done
printf 'old-unit\n' >"${CASE_DIR}/targets/dedao-evolution-worker@.service"
touch "${CASE_DIR}/state/fail-start-dedao-evolution-worker@evaluation-worker.service-once"
touch "${CASE_DIR}/state/fail-rollback-evaluation-install-always"
if run_cutover; then fail "rollback install failure unexpectedly succeeded"; fi
[[ -f "${CASE_DIR}/state/rollback-install-failed" ]] || fail "rollback install failure was not injected"
if [[ "$(grep -c '^daemon-reload$' "${CASE_DIR}/state/systemctl.log")" != 1 ]]; then
  fail "rollback continued after a failed binary restore"
fi

printf 'evolution worker cutover behavior smoke passed\n'
