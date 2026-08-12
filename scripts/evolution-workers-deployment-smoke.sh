#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT="${ROOT}/deploy/systemd/dedao-evolution-worker@.service"
CUTOVER="${ROOT}/scripts/evolution-workers-direct-cutover.sh"
BEHAVIOR="${ROOT}/scripts/evolution-workers-direct-cutover-behavior-smoke.sh"
README="${ROOT}/README.md"
WORKFLOW="${ROOT}/.github/workflows/kbase-build-gates.yml"

fail() {
  printf 'evolution worker deployment smoke: %s\n' "$*" >&2
  exit 1
}

[[ -f "$UNIT" ]] || fail "systemd template is missing"
[[ -x "$CUTOVER" ]] || fail "cutover script is missing or not executable"
[[ -x "$BEHAVIOR" ]] || fail "behavior smoke is missing or not executable"

for required in \
  'Description=KBase %i evolution worker' \
  'After=network-online.target dedao-kbase.service' \
  'Wants=dedao-kbase.service' \
  'EnvironmentFile=/etc/dedao-kbase/kbase.env' \
  'Environment=KBASE_EVOLUTION_WORKER_ID=%H-%i' \
  'ExecStartPre=/opt/dedao-kbase/bin/%i check-live' \
  'ExecStart=/opt/dedao-kbase/bin/%i run' \
  'Restart=always' \
  'KillSignal=SIGTERM'
do
  grep -Fq "$required" "$UNIT" || fail "systemd template is missing: ${required}"
done

if grep -Eq '^(Requires|PartOf|BindsTo)=.*dedao-kbase\.service' "$UNIT"; then
  fail "evolution workers must survive a KBase restart"
fi

for binary in agent-evolution-worker knowledge-evolution-worker evaluation-worker; do
  grep -Fq "./cmd/${binary}" "$README" || fail "README does not build ${binary}"
  grep -Fq "./cmd/${binary}" "$WORKFLOW" || fail "CI does not build ${binary}"
  grep -Fq "KBASE_EVOLUTION_BINARY_DIR:?}/${binary}\" build-info" "$README" || fail "README does not verify ${binary} revision from the install directory"
done

for required in \
  'KBASE_AGENT_EVOLUTION_CANDIDATE_BIN="${KBASE_AGENT_EVOLUTION_CANDIDATE_BIN:?}"' \
  'KBASE_KNOWLEDGE_EVOLUTION_CANDIDATE_BIN="${KBASE_KNOWLEDGE_EVOLUTION_CANDIDATE_BIN:?}"' \
  'KBASE_EVALUATION_CANDIDATE_BIN="${KBASE_EVALUATION_CANDIDATE_BIN:?}"' \
  'KBASE_EVOLUTION_UNIT_SHA256' \
  'KBASE_EVOLUTION_REVISION' \
  'check-live'
do
  grep -Fq "$required" "$README" || fail "README is missing deployment contract: ${required}"
done

grep -Fq 'worker_id="$(hostname)-${worker}"' "$README" || fail "README preflight does not reuse the stable systemd worker ID"
if grep -Fq 'preflight-${worker}' "$README"; then
  fail "README creates persistent temporary preflight Agent identities"
fi

for required in \
  'KBASE_EVOLUTION_UNIT_SHA256' \
  'KBASE_EVOLUTION_REVISION'
do
  grep -Fq "$required" "$CUTOVER" || fail "cutover does not require ${required}"
done

for required in \
  'scripts/evolution-workers-direct-cutover.sh' \
  'scripts/evolution-workers-direct-cutover-behavior-smoke.sh'
do
  grep -Fq "$required" "$README" || fail "README is missing deployment contract: ${required}"
  grep -Fq "$required" "$WORKFLOW" || fail "CI is missing deployment gate: ${required}"
done

for required in \
  'dedao-evolution-worker@agent-evolution-worker.service' \
  'dedao-evolution-worker@knowledge-evolution-worker.service' \
  'dedao-evolution-worker@evaluation-worker.service'
do
  grep -Fq "$required" "$README" || fail "README is missing deployment contract: ${required}"
done

bash -n "$CUTOVER"
bash "$BEHAVIOR"
printf 'evolution worker deployment smoke passed\n'
