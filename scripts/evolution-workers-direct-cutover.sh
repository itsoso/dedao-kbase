#!/usr/bin/env bash

set -Eeuo pipefail

fail() {
  printf 'evolution worker direct cutover: %s\n' "$*" >&2
  exit 1
}

require_variable() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "${name} is required"
}

for variable in \
  KBASE_EVOLUTION_BACKUP_DIR \
  KBASE_EVOLUTION_BINARY_DIR \
  KBASE_EVOLUTION_UNIT_TARGET \
  KBASE_EVOLUTION_UNIT_CANDIDATE_SOURCE \
  KBASE_EVOLUTION_UNIT_SHA256 \
  KBASE_EVOLUTION_REVISION \
  KBASE_AGENT_EVOLUTION_CANDIDATE_BIN \
  KBASE_KNOWLEDGE_EVOLUTION_CANDIDATE_BIN \
  KBASE_EVALUATION_CANDIDATE_BIN \
  KBASE_AGENT_EVOLUTION_SHA256 \
  KBASE_KNOWLEDGE_EVOLUTION_SHA256 \
  KBASE_EVALUATION_SHA256
do
  require_variable "$variable"
done

binaries=(agent-evolution-worker knowledge-evolution-worker evaluation-worker)
candidates=(
  "${KBASE_AGENT_EVOLUTION_CANDIDATE_BIN:?}"
  "${KBASE_KNOWLEDGE_EVOLUTION_CANDIDATE_BIN:?}"
  "${KBASE_EVALUATION_CANDIDATE_BIN:?}"
)
hashes=(
  "${KBASE_AGENT_EVOLUTION_SHA256:?}"
  "${KBASE_KNOWLEDGE_EVOLUTION_SHA256:?}"
  "${KBASE_EVALUATION_SHA256:?}"
)
unit_candidate_target="${KBASE_EVOLUTION_UNIT_TARGET:?}.candidate"
backup_created=0
stopping_started=0
services_quiesced=0
replacement_started=0

service_name() {
  printf 'dedao-evolution-worker@%s.service\n' "$1"
}

service_enabled_state() {
  local service="$1" result_variable="$2" output status=0 resolved_state restore_error_trap=0
  if [[ -n "$(trap -p ERR)" ]]; then
    restore_error_trap=1
    trap - ERR
  fi
  if output="$(sudo systemctl is-enabled "$service" 2>/dev/null)"; then
    status=0
  else
    status=$?
  fi
  if ((restore_error_trap == 1)); then
    trap handle_cutover_error ERR
  fi
  case "$output" in
    enabled|enabled-runtime|linked|linked-runtime) resolved_state=enabled ;;
    disabled|static|indirect|generated|transient|alias|masked|masked-runtime|not-found) resolved_state=disabled ;;
    *) printf 'evolution worker direct cutover: cannot determine enablement for %s (exit %d)\n' "$service" "$status" >&2; return 1 ;;
  esac
  printf -v "$result_variable" '%s' "$resolved_state"
}

service_active_state() {
  local service="$1" result_variable="$2" output status=0 resolved_state restore_error_trap=0
  if [[ -n "$(trap -p ERR)" ]]; then
    restore_error_trap=1
    trap - ERR
  fi
  if output="$(sudo systemctl is-active "$service" 2>/dev/null)"; then
    status=0
  else
    status=$?
  fi
  if ((restore_error_trap == 1)); then
    trap handle_cutover_error ERR
  fi
  case "$output" in
    active) resolved_state=active ;;
    inactive|failed|unknown) resolved_state=inactive ;;
    *) printf 'evolution worker direct cutover: cannot determine activity for %s (exit %d)\n' "$service" "$status" >&2; return 1 ;;
  esac
  printf -v "$result_variable" '%s' "$resolved_state"
}

stop_service_strict() {
  local service="$1" state
  service_active_state "$service" state || return 1
  if [[ "$state" == inactive ]]; then
    return 0
  fi
  sudo systemctl stop "$service" || return 1
  service_active_state "$service" state || return 1
  [[ "$state" == inactive ]]
}

disable_service_strict() {
  local service="$1" state
  service_enabled_state "$service" state || return 1
  if [[ "$state" == disabled ]]; then
    return 0
  fi
  sudo systemctl disable "$service" || return 1
  service_enabled_state "$service" state || return 1
  [[ "$state" == disabled ]]
}

restore_service_states() {
  local binary service state
  for binary in "${binaries[@]}"; do
    service="$(service_name "$binary")"
    if sudo test -f "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.enabled"; then
      sudo systemctl enable "$service" || return 1
    else
      disable_service_strict "$service" || return 1
    fi
    if sudo test -f "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.active"; then
      sudo systemctl start "$service" || return 1
      service_active_state "$service" state || return 1
      [[ "$state" == active ]] || return 1
    else
      stop_service_strict "$service" || return 1
    fi
  done
}

remove_staging() {
  local binary
  for binary in "${binaries[@]}"; do
    sudo rm -f "${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}.candidate" || return 1
  done
  sudo rm -f "$unit_candidate_target" || return 1
}

remove_partial_backup() {
  local binary suffix
  ((backup_created == 1)) || return 0
  for binary in "${binaries[@]}"; do
    sudo rm -f "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}" || return 1
    for suffix in present absent enabled disabled active inactive; do
      sudo rm -f "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.${suffix}" || return 1
    done
  done
  sudo rm -f \
    "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service" \
    "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service.present" \
    "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service.absent" || return 1
  sudo rmdir "${KBASE_EVOLUTION_BACKUP_DIR:?}" || return 1
}

rollback_evolution_workers() {
  local binary target
  for binary in "${binaries[@]}"; do
    stop_service_strict "$(service_name "$binary")" || return 1
  done
  for binary in "${binaries[@]}"; do
    target="${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}"
    if sudo test -f "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.present"; then
      sudo install -o root -g root -m 0755 \
        "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}" "$target" || return 1
    else
      sudo rm -f "$target" || return 1
    fi
  done
  if sudo test -f "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service.present"; then
    sudo install -o root -g root -m 0644 \
      "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service" \
      "${KBASE_EVOLUTION_UNIT_TARGET:?}" || return 1
  else
    sudo rm -f "${KBASE_EVOLUTION_UNIT_TARGET:?}" || return 1
  fi
  remove_staging || return 1
  sudo systemctl daemon-reload || return 1
  restore_service_states || return 1
}

fail_after_replacement() {
  local message="$1"
  trap - ERR
  rollback_evolution_workers || fail "${message}; rollback failed"
  fail "$message"
}

handle_cutover_error() {
  local status=$?
  trap - ERR
  if ((replacement_started == 1)); then
    rollback_evolution_workers || exit "$status"
  elif ((services_quiesced == 1)); then
    restore_service_states || exit "$status"
    remove_staging
    remove_partial_backup
  elif ((stopping_started == 1)); then
    # No installed file changed. Restore any service stopped before a later
    # stop failed, then remove partial staging so the same release can retry.
    restore_service_states || exit "$status"
    remove_staging
    remove_partial_backup
  else
    remove_staging
    remove_partial_backup
  fi
  exit "$status"
}

test ! -e "${KBASE_EVOLUTION_BACKUP_DIR:?}" || fail "backup directory already exists"
test -d "${KBASE_EVOLUTION_BINARY_DIR:?}" || fail "evolution binary target directory is missing"
test -d "$(dirname -- "${KBASE_EVOLUTION_UNIT_TARGET:?}")" || fail "systemd target directory is missing"
test -f "${KBASE_EVOLUTION_UNIT_CANDIDATE_SOURCE:?}" || fail "candidate systemd template is missing"
test ! -e "$unit_candidate_target" || fail "candidate systemd staging target already exists"
test "$(sha256sum "${KBASE_EVOLUTION_UNIT_CANDIDATE_SOURCE:?}" | awk '{print $1}')" = "${KBASE_EVOLUTION_UNIT_SHA256:?}" || fail "candidate systemd template SHA-256 mismatch"

for index in "${!binaries[@]}"; do
  binary="${binaries[$index]}"
  candidate="${candidates[$index]}"
  target="${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}"
  test -x "$candidate" || fail "candidate ${binary} is missing or not executable"
  test ! -e "${target}.candidate" || fail "candidate ${binary} staging target already exists"
  test "$(sha256sum "$candidate" | awk '{print $1}')" = "${hashes[$index]}" || fail "candidate ${binary} SHA-256 mismatch"
  build_info="$("$candidate" build-info)" || fail "candidate ${binary} build-info failed"
  printf '%s' "$build_info" | grep -Fq "\"component\":\"${binary}\"" || fail "candidate ${binary} component mismatch"
  printf '%s' "$build_info" | grep -Fq "\"revision\":\"${KBASE_EVOLUTION_REVISION:?}\"" || fail "candidate ${binary} revision mismatch"
done

trap handle_cutover_error ERR
sudo install -d -o root -g root -m 0700 "${KBASE_EVOLUTION_BACKUP_DIR:?}"
backup_created=1
for binary in "${binaries[@]}"; do
  target="${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}"
  service="$(service_name "$binary")"
  if test -f "$target"; then
    sudo install -o root -g root -m 0755 "$target" "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}"
    sudo touch "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.present"
  else
    sudo touch "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.absent"
  fi
  service_enabled_state "$service" enabled_state
  service_active_state "$service" active_state
  sudo touch "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.${enabled_state}"
  sudo touch "${KBASE_EVOLUTION_BACKUP_DIR:?}/${binary}.${active_state}"
done
if test -f "${KBASE_EVOLUTION_UNIT_TARGET:?}"; then
  sudo install -o root -g root -m 0644 \
    "${KBASE_EVOLUTION_UNIT_TARGET:?}" \
    "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service"
  sudo touch "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service.present"
else
  sudo touch "${KBASE_EVOLUTION_BACKUP_DIR:?}/dedao-evolution-worker@.service.absent"
fi

for index in "${!binaries[@]}"; do
  binary="${binaries[$index]}"
  sudo install -o root -g root -m 0755 \
    "${candidates[$index]}" \
    "${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}.candidate"
  test "$(sha256sum "${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}.candidate" | awk '{print $1}')" = "${hashes[$index]}"
done
sudo install -o root -g root -m 0644 \
  "${KBASE_EVOLUTION_UNIT_CANDIDATE_SOURCE:?}" \
  "$unit_candidate_target"
test "$(sha256sum "$unit_candidate_target" | awk '{print $1}')" = "${KBASE_EVOLUTION_UNIT_SHA256:?}"

stopping_started=1
for binary in "${binaries[@]}"; do
  stop_service_strict "$(service_name "$binary")"
done
services_quiesced=1
replacement_started=1
for binary in "${binaries[@]}"; do
  sudo mv \
    "${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}.candidate" \
    "${KBASE_EVOLUTION_BINARY_DIR:?}/${binary}"
done
sudo mv "$unit_candidate_target" "${KBASE_EVOLUTION_UNIT_TARGET:?}"
sudo systemctl daemon-reload
for binary in "${binaries[@]}"; do
  service="$(service_name "$binary")"
  sudo systemctl enable "$service"
  sudo systemctl start "$service"
done
sleep 3 || fail_after_replacement "stability wait failed"
for binary in "${binaries[@]}"; do
  service="$(service_name "$binary")"
  service_enabled_state "$service" enabled_state || fail_after_replacement "cannot verify ${service} enablement"
  service_active_state "$service" active_state || fail_after_replacement "cannot verify ${service} activity"
  [[ "$enabled_state" == enabled ]] || fail_after_replacement "${service} is not enabled after cutover"
  [[ "$active_state" == active ]] || fail_after_replacement "${service} is not active after cutover"
  if ! restart_count="$(sudo systemctl show "$service" --property=NRestarts --value)"; then
    fail_after_replacement "cannot read ${service} restart count"
  fi
  [[ "$restart_count" == 0 ]] || fail_after_replacement "${service} restarted during stability window"
done
trap - ERR
