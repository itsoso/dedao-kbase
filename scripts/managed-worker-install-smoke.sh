#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/managed-worker-install.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$script_dir/lib/managed-worker-pair.sh"
# shellcheck source=scripts/lib/managed-worker-install.sh
source "$script_dir/lib/managed-worker-install.sh"

symlink_home="$tmp_dir/symlink-home"
outside_root="$tmp_dir/outside-root"
mkdir -p "$symlink_home/Library/Application Support" "$outside_root"
symlink_home="$(cd "$symlink_home" && pwd -P)"
ln -s "$outside_root" "$symlink_home/Library/Application Support/KBase"
_managed_worker_install_set_home_paths "$symlink_home"
if ! declare -F _managed_worker_install_prepare_root >/dev/null; then
  echo "managed install has no safe transaction-root preparation" >&2
  exit 1
fi
if _managed_worker_install_prepare_root || [[ ! -L "$symlink_home/Library/Application Support/KBase" ]]; then
  echo "managed install followed a symlink transaction root" >&2
  exit 1
fi

home="$tmp_dir/home"
mkdir -p "$home"
home="$(cd "$home" && pwd -P)"
_managed_worker_install_set_home_paths "$home"
mkdir -p "$MANAGED_WORKER_INSTALL_ROOT"

assert_stale_lock_reclaimed() {
  local description="$1"
  if ! _managed_worker_install_reclaim_stale_lock || [[ ! -f "$MANAGED_WORKER_INSTALL_LOCK" ]]; then
    echo "managed install did not reclaim $description" >&2
    exit 1
  fi
}

printf 'not-a-pid\n' >"$MANAGED_WORKER_INSTALL_LOCK"
assert_stale_lock_reclaimed "an unlocked advisory lock with malformed old contents"
rm -f "$MANAGED_WORKER_INSTALL_LOCK"

printf '%040d\n' 1 >"$MANAGED_WORKER_INSTALL_LOCK"
assert_stale_lock_reclaimed "an unlocked advisory lock with oversized old contents"
rm -f "$MANAGED_WORKER_INSTALL_LOCK"

printf '999999\n' >"$MANAGED_WORKER_INSTALL_LOCK"
assert_stale_lock_reclaimed "a dead-pid lock"

printf 'stale\n' >"$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-ready.999999"
printf 'stale\n' >"$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-release.999999"
if ! _managed_worker_install_try_acquire_lock ||
  [[ -e "$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-ready.999999" ||
    -e "$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-release.999999" ]]; then
  echo "managed install did not clean power-loss lock-helper markers" >&2
  exit 1
fi
_managed_worker_install_release_lock

ln -s "$tmp_dir/marker-target" "$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-ready.999998"
if _managed_worker_install_try_acquire_lock ||
  [[ ! -L "$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-ready.999998" ]]; then
  echo "managed install did not fail closed on a symlink lock-helper marker" >&2
  exit 1
fi
rm -f "$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-ready.999998"

_managed_worker_install_try_acquire_lock
kill -TERM "$MANAGED_WORKER_INSTALL_LOCK_HELPER_PID"
sleep 0.1
if /usr/bin/lockf -s -t 0 -k "$MANAGED_WORKER_INSTALL_LOCK" /usr/bin/true; then
  echo "managed install helper released the lock on a cooperative group signal" >&2
  exit 1
fi
_managed_worker_install_release_lock

INSTALL_LOCK="$MANAGED_WORKER_INSTALL_LOCK" INSTALL_LOCK_READY="$tmp_dir/install-lock-ready" /bin/bash -c '
  exec 8>>"$INSTALL_LOCK"
  /usr/bin/lockf -s -t 0 8
  : >"$INSTALL_LOCK_READY"
  sleep 5
' &
live_lock_pid=$!
for ((attempt = 0; attempt < 100; attempt++)); do
  [[ -e "$tmp_dir/install-lock-ready" ]] && break
  sleep 0.01
done
if _managed_worker_install_reclaim_stale_lock || [[ ! -f "$MANAGED_WORKER_INSTALL_LOCK" ]]; then
  kill "$live_lock_pid" 2>/dev/null || true
  echo "managed install reclaimed a live lock" >&2
  exit 1
fi
kill "$live_lock_pid"
wait "$live_lock_pid" 2>/dev/null || true
rm -f "$MANAGED_WORKER_INSTALL_LOCK"

mkdir "$tmp_dir/lock-target"
ln -s "$tmp_dir/lock-target" "$MANAGED_WORKER_INSTALL_LOCK"
if _managed_worker_install_reclaim_stale_lock || [[ ! -L "$MANAGED_WORKER_INSTALL_LOCK" ]]; then
  echo "managed install reclaimed a symlink lock" >&2
  exit 1
fi
rm -f "$MANAGED_WORKER_INSTALL_LOCK"

acquire_script="$tmp_dir/acquire.sh"
cat >"$acquire_script" <<'ACQUIRE'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
source "${INSTALL_LIBRARY:?}"
_managed_worker_install_set_home_paths "${INSTALL_HOME:?}"
if _managed_worker_install_try_acquire_lock; then
  printf '%s\n' "$$" >>"${WINNERS:?}"
  sleep 1
  _managed_worker_install_release_lock
fi
ACQUIRE
chmod 0755 "$acquire_script"
winners="$tmp_dir/winners"
printf '999999\n' >"$MANAGED_WORKER_INSTALL_LOCK"
PAIR_LIBRARY="$script_dir/lib/managed-worker-pair.sh" INSTALL_LIBRARY="$script_dir/lib/managed-worker-install.sh" \
  INSTALL_HOME="$home" WINNERS="$winners" "$acquire_script" &
first_pid=$!
PAIR_LIBRARY="$script_dir/lib/managed-worker-pair.sh" INSTALL_LIBRARY="$script_dir/lib/managed-worker-install.sh" \
  INSTALL_HOME="$home" WINNERS="$winners" "$acquire_script" &
second_pid=$!
wait "$first_pid"
wait "$second_pid"
winner_count="$(wc -l <"$winners")"
winner_count="${winner_count//[[:space:]]/}"
if [[ "$winner_count" != 1 ]]; then
  echo "managed install atomic lock admitted $winner_count simultaneous owners" >&2
  exit 1
fi

install_inherit_script="$tmp_dir/install-inherit.sh"
cat >"$install_inherit_script" <<'INSTALL_INHERIT'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
source "${INSTALL_LIBRARY:?}"
_managed_worker_install_set_home_paths "${INSTALL_HOME:?}"
_managed_worker_install_try_acquire_lock
sleep 5 &
printf '%s\n' "$!" >"${CHILD_PID_FILE:?}"
: >"${READY_FILE:?}"
wait
INSTALL_INHERIT
chmod 0755 "$install_inherit_script"
install_supervisor_script="$tmp_dir/install-supervisor.sh"
cat >"$install_supervisor_script" <<'INSTALL_SUPERVISOR'
#!/bin/bash
set -euo pipefail
"${OWNER_SCRIPT:?}" &
owner_pid=$!
printf '%s\n' "$owner_pid" >"${OWNER_PID_FILE:?}"
while [[ ! -e "${READY_FILE:?}" ]]; do sleep 0.01; done
: >"${SUPERVISOR_STOPPING:?}"
kill -STOP $$
wait "$owner_pid"
INSTALL_SUPERVISOR
chmod 0755 "$install_supervisor_script"
PAIR_LIBRARY="$script_dir/lib/managed-worker-pair.sh" INSTALL_LIBRARY="$script_dir/lib/managed-worker-install.sh" \
  INSTALL_HOME="$home" CHILD_PID_FILE="$tmp_dir/install-inherited-child" READY_FILE="$tmp_dir/install-inherit-ready" \
  OWNER_SCRIPT="$install_inherit_script" OWNER_PID_FILE="$tmp_dir/install-owner-pid" \
  SUPERVISOR_STOPPING="$tmp_dir/install-supervisor-stopping" "$install_supervisor_script" &
install_lock_supervisor=$!
for ((attempt = 0; attempt < 100; attempt++)); do
  [[ -e "$tmp_dir/install-supervisor-stopping" ]] && break
  sleep 0.01
done
sleep 0.1
install_lock_parent="$(<"$tmp_dir/install-owner-pid")"
install_inherited_child="$(<"$tmp_dir/install-inherited-child")"
kill -KILL "$install_lock_parent"
install_reacquired=false
for ((attempt = 0; attempt < 100; attempt++)); do
  if _managed_worker_install_try_acquire_lock; then install_reacquired=true; break; fi
  sleep 0.01
done
if [[ "$install_reacquired" != true ]]; then
  kill "$install_inherited_child" 2>/dev/null || true
  kill -CONT "$install_lock_supervisor" 2>/dev/null || true
  wait "$install_lock_supervisor" 2>/dev/null || true
  echo "managed install child inherited the lock after owner SIGKILL" >&2
  exit 1
fi
_managed_worker_install_release_lock
kill -CONT "$install_lock_supervisor" 2>/dev/null || true
wait "$install_lock_supervisor" 2>/dev/null || true
kill "$install_inherited_child" 2>/dev/null || true

fake_security="$tmp_dir/security"
cat >"$fake_security" <<'SECURITY'
#!/bin/bash
set -euo pipefail
if [[ -n "${SECURITY_ENV_CAPTURE:-}" && -n "${MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE+x}" ]]; then
  printf 'present:%s\n' "$MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE" >>"$SECURITY_ENV_CAPTURE"
fi
if [[ -n "${SECURITY_ENV_CAPTURE:-}" && -n "${value+x}" ]]; then
  printf 'value-present:%s\n' "$value" >>"$SECURITY_ENV_CAPTURE"
fi
operation="${1:?}"
shift
account=""
while (($# > 0)); do
  case "$1" in
    -a) account="${2:?}"; shift 2 ;;
    *) shift ;;
  esac
done
case "$account" in
  transport-token) item="${KEYCHAIN_DIR:?}/main" ;;
  transport-token-install-backup) item="${KEYCHAIN_DIR:?}/backup" ;;
  *) exit 64 ;;
esac
case "$operation" in
  find-generic-password)
    [[ -f "$item" ]] || exit 44
    /bin/cat "$item"
    ;;
  add-generic-password)
    IFS= read -r value
    IFS= read -r confirmation
    [[ "$value" == "$confirmation" ]]
    printf '%s\n' "$value" >"$item"
    ;;
  delete-generic-password)
    [[ -f "$item" ]] || exit 44
    /bin/rm -f "$item"
    ;;
  *) exit 64 ;;
esac
SECURITY

fake_launchctl="$tmp_dir/launchctl"
cat >"$fake_launchctl" <<'LAUNCHCTL'
#!/bin/bash
set -euo pipefail
operation="${1:?}"
case "$operation" in
  print)
    [[ ! -e "${PRINT_FAIL_ARMED:?}" ]] || exit 88
    if [[ -f "${SERVICE_STATE:?}" && "$(<"$SERVICE_STATE")" == loaded ]]; then exit 0; else exit 113; fi
    ;;
  bootout)
    [[ ! -e "${ROLLBACK_FAIL_ARMED:?}" ]] || exit 98
    printf 'unloaded\n' >"${SERVICE_STATE:?}"
    ;;
  bootstrap)
    [[ ! -f "${SERVICE_STATE:?}" || "$(<"$SERVICE_STATE")" != loaded ]] || exit 36
    printf 'loaded\n' >"$SERVICE_STATE"
    ;;
  kickstart) ;;
  *) exit 64 ;;
esac
LAUNCHCTL
chmod 0755 "$fake_security" "$fake_launchctl"

transaction_script="$tmp_dir/transaction.sh"
cat >"$transaction_script" <<'TRANSACTION'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
source "${INSTALL_LIBRARY:?}"
_managed_worker_install_security() { "${FAKE_SECURITY:?}" "$@"; }
_managed_worker_install_launchctl() { "${FAKE_LAUNCHCTL:?}" "$@"; }
if [[ "${MODE:?}" == cleanup-kill ]]; then
  _managed_worker_install_after_journal_removal() { kill -KILL $$; }
fi

begin_transaction() {
  managed_worker_install_begin "${INSTALL_HOME:?}" source-agent "${WORKER_DEST:?}" "${UPDATER_DEST:?}" \
    "${PLIST_DEST:?}" "gui/501" "life.executor.kbase.source-agent"
}

mutate_transaction() {
  local new_token=""
  IFS= read -r new_token
  printf 'new-worker' >"${WORKER_DEST}.new"
  printf 'new-updater' >"${UPDATER_DEST}.new"
  begin_transaction
  managed_worker_pair_publish "${WORKER_DEST}.new" "${UPDATER_DEST}.new" "$WORKER_DEST" "$UPDATER_DEST"
  managed_worker_install_mark published
  managed_worker_install_mark keychain
  _managed_worker_install_write_keychain_value transport-token "$new_token"
  unset new_token
  managed_worker_install_mark plist
  printf 'new-plist' >"$PLIST_DEST"
  managed_worker_install_mark launching
  if [[ "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" == 1 ]]; then
    _managed_worker_install_launchctl bootout "gui/501/life.executor.kbase.source-agent"
  fi
  _managed_worker_install_launchctl bootstrap gui/501 "$PLIST_DEST"
}

case "${MODE:?}" in
  term)
    trap 'managed_worker_install_rollback; exit 143' TERM
    mutate_transaction
    kill -TERM $$
    ;;
  kill)
    mutate_transaction
    kill -KILL $$
    ;;
  committing)
    mutate_transaction
    managed_worker_install_mark committing
    kill -KILL $$
    ;;
  rollback-fail)
    mutate_transaction
    : >"${ROLLBACK_FAIL_ARMED:?}"
    if managed_worker_install_rollback; then exit 88; else exit 97; fi
    ;;
  rollback-print-fail)
    mutate_transaction
    : >"${PRINT_FAIL_ARMED:?}"
    if managed_worker_install_rollback; then exit 88; else exit 97; fi
    ;;
  cleanup-kill)
    mutate_transaction
    managed_worker_install_rollback
    ;;
  begin-only)
    begin_transaction
    managed_worker_install_rollback
    ;;
  direct-write)
    _managed_worker_install_write_keychain_value transport-token old-keychain-token
    ;;
  recover)
    begin_transaction
    managed_worker_install_rollback
    ;;
  hold)
    begin_transaction
    printf 'holder-acquired\n' >>"${ORDER_FILE:?}"
    sleep 1
    managed_worker_install_rollback
    printf 'holder-released\n' >>"$ORDER_FILE"
    ;;
  wait)
    begin_transaction
    printf 'waiter-acquired\n' >>"${ORDER_FILE:?}"
    managed_worker_install_rollback
    ;;
  *) exit 64 ;;
esac
TRANSACTION
chmod 0755 "$transaction_script"

transaction_home="$tmp_dir/transaction-home"
mkdir -p "$transaction_home/bin" "$transaction_home/Library/LaunchAgents" "$transaction_home/keychain"
transaction_home="$(cd "$transaction_home" && pwd -P)"
transaction_worker="$transaction_home/bin/source-agent"
transaction_updater="$transaction_home/bin/source-agent-updater"
transaction_plist="$transaction_home/Library/LaunchAgents/life.executor.kbase.source-agent.plist"
keychain_dir="$transaction_home/keychain"
service_state="$transaction_home/service-state"

reset_transaction() {
  printf 'old-worker' >"$transaction_worker"
  printf 'old-updater' >"$transaction_updater"
  printf 'old-plist' >"$transaction_plist"
  printf 'old-token\n' >"$keychain_dir/main"
  rm -f "$keychain_dir/backup"
  printf 'loaded\n' >"$service_state"
}

assert_old_transaction() {
  if [[ "$(<"$transaction_worker")" != old-worker ]]; then echo "old worker was not restored" >&2; exit 1; fi
  if [[ "$(<"$transaction_updater")" != old-updater ]]; then echo "old updater was not restored" >&2; exit 1; fi
  if [[ "$(<"$transaction_plist")" != old-plist ]]; then echo "old plist was not restored" >&2; exit 1; fi
  if [[ ! -f "$keychain_dir/main" || "$(<"$keychain_dir/main")" != old-token ]]; then echo "old token was not restored" >&2; exit 1; fi
  if [[ "$(<"$service_state")" != loaded ]]; then echo "old service was not restored" >&2; exit 1; fi
  if [[ -e "$keychain_dir/backup" ]]; then echo "backup token was not removed" >&2; exit 1; fi
}

assert_transaction_clean() {
  local artifact
  if [[ -e "$transaction_home/Library/Application Support/KBase/.managed-worker-install-journal" ||
    -e "$transaction_home/Library/Application Support/KBase/.managed-worker-install-journal.tmp" ||
    -e "$transaction_home/Library/Application Support/KBase/.managed-worker-install-plist-old" ]]; then
    echo "managed install recovery left transaction state" >&2
    exit 1
  fi
  if compgen -G "$transaction_home/Library/Application Support/KBase/.managed-worker-install.lock-ready.*" >/dev/null ||
    compgen -G "$transaction_home/Library/Application Support/KBase/.managed-worker-install.lock-release.*" >/dev/null; then
    echo "managed install recovery left lock-helper state" >&2
    exit 1
  fi
  for artifact in "$transaction_home/bin"/.*.pair-*; do
    [[ -e "$artifact" ]] || continue
    if [[ "$artifact" != "$transaction_home/bin/.source-agent-updater.pair-lock" ]]; then
      echo "managed install recovery left pair transaction state" >&2
      exit 1
    fi
  done
}

run_transaction() {
  local mode="$1"
  PAIR_LIBRARY="$script_dir/lib/managed-worker-pair.sh" INSTALL_LIBRARY="$script_dir/lib/managed-worker-install.sh" \
    FAKE_SECURITY="$fake_security" FAKE_LAUNCHCTL="$fake_launchctl" KEYCHAIN_DIR="$keychain_dir" SERVICE_STATE="$service_state" \
    ROLLBACK_FAIL_ARMED="$tmp_dir/rollback-fail-armed" \
    PRINT_FAIL_ARMED="$tmp_dir/print-fail-armed" \
    INSTALL_HOME="$transaction_home" WORKER_DEST="$transaction_worker" UPDATER_DEST="$transaction_updater" PLIST_DEST="$transaction_plist" \
    MODE="$mode" ORDER_FILE="${ORDER_FILE:-$tmp_dir/no-order}" \
    "$transaction_script"
}

recover_or_fail() {
  local description="$1" output status
  set +e
  output="$(run_transaction recover 2>&1)"
  status=$?
  set -e
  if [[ $status -ne 0 ]]; then
    echo "managed install $description recovery exited $status" >&2
    printf '%s\n' "$output" >&2
    exit 1
  fi
}

reset_transaction
security_env_capture="$tmp_dir/security-env-capture"
export SECURITY_ENV_CAPTURE="$security_env_capture"
export MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE="caller-preexported-secret"
set +e
printf 'unused-token\n' | run_transaction begin-only >/dev/null 2>&1
preexport_status=$?
set -e
unset SECURITY_ENV_CAPTURE MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE
if [[ $preexport_status -ne 0 ]]; then
  echo "managed install preexport regression fixture exited $preexport_status" >&2
  exit 1
fi
if [[ -e "$security_env_capture" ]]; then
  echo "managed install exposed Keychain scratch state to a security child environment" >&2
  exit 1
fi
assert_old_transaction
assert_transaction_clean

reset_transaction
security_env_capture="$tmp_dir/security-value-env-capture"
export SECURITY_ENV_CAPTURE="$security_env_capture"
export value="caller-preexported-value"
set +e
run_transaction direct-write >/dev/null 2>&1
preexport_value_status=$?
set -e
unset SECURITY_ENV_CAPTURE value
if [[ $preexport_value_status -ne 0 ]]; then
  echo "managed install preexported value regression fixture exited $preexport_value_status" >&2
  exit 1
fi
if [[ ! -f "$security_env_capture" || "$(<"$security_env_capture")" != "value-present:caller-preexported-value" ]]; then
  echo "managed install exposed a local Keychain secret through an inherited export attribute" >&2
  exit 1
fi

reset_transaction
set +e
printf 'new-token\n' | run_transaction term >"$tmp_dir/term.stdout" 2>"$tmp_dir/term.stderr"
term_status=$?
set -e
if [[ $term_status -ne 143 ]]; then
  echo "managed install SIGTERM fixture exited $term_status" >&2
  sed -n '1,20p' "$tmp_dir/term.stderr" >&2
  exit 1
fi
assert_old_transaction
assert_transaction_clean

reset_transaction
: >"$tmp_dir/print-fail-armed"
set +e
printf 'unused-token\n' | run_transaction begin-only >/dev/null 2>&1
snapshot_print_status=$?
set -e
rm -f "$tmp_dir/print-fail-armed"
if [[ $snapshot_print_status -eq 0 ]]; then
  echo "managed install accepted an operational launchctl snapshot failure" >&2
  exit 1
fi
assert_old_transaction
assert_transaction_clean

reset_transaction
rm -f "$transaction_plist"
set +e
printf 'unused-token\n' | run_transaction begin-only >/dev/null 2>&1
loaded_without_plist_status=$?
set -e
if [[ $loaded_without_plist_status -eq 0 ]]; then
  echo "managed install accepted a loaded service without a restorable plist" >&2
  exit 1
fi
if [[ "$(<"$transaction_worker")" != old-worker || "$(<"$transaction_updater")" != old-updater ||
  "$(<"$keychain_dir/main")" != old-token || "$(<"$service_state")" != loaded ]]; then
  echo "managed install changed state while rejecting a loaded service without a plist" >&2
  exit 1
fi
assert_transaction_clean

reset_transaction
set +e
printf 'new-token\n' | run_transaction kill >/dev/null 2>&1
kill_status=$?
set -e
if [[ $kill_status -eq 0 ]]; then
  echo "managed install SIGKILL fixture unexpectedly succeeded" >&2
  exit 1
fi
plist_backup_mode="$(stat -f '%Lp' "$transaction_home/Library/Application Support/KBase/.managed-worker-install-plist-old")"
if [[ "$plist_backup_mode" != 600 ]]; then
  echo "managed install plist backup mode was $plist_backup_mode, want 600" >&2
  exit 1
fi
recover_or_fail "non-committing SIGKILL"
assert_old_transaction
assert_transaction_clean

reset_transaction
set +e
printf 'committed-token\n' | run_transaction committing >/dev/null 2>&1
commit_kill_status=$?
set -e
if [[ $commit_kill_status -eq 0 ]]; then
  echo "managed install committing SIGKILL fixture unexpectedly succeeded" >&2
  exit 1
fi
recover_or_fail "committing SIGKILL"
if [[ "$(<"$transaction_worker")" != new-worker || "$(<"$transaction_updater")" != new-updater ||
  "$(<"$transaction_plist")" != new-plist || "$(<"$keychain_dir/main")" != committed-token || "$(<"$service_state")" != loaded ]]; then
  echo "managed install did not finish a committing crash forward" >&2
  exit 1
fi
assert_transaction_clean

reset_transaction
rm -f "$tmp_dir/print-fail-armed"
set +e
rollback_print_output="$(printf 'new-token\n' | run_transaction rollback-print-fail 2>&1)"
rollback_print_status=$?
set -e
if [[ $rollback_print_status -ne 97 || "$rollback_print_output" != "managed worker installation rollback failed" ]]; then
  echo "managed install rollback launchctl probe failure was not fixed and recoverable" >&2
  exit 1
fi
if [[ ! -f "$transaction_home/Library/Application Support/KBase/.managed-worker-install-journal" ]]; then
  echo "managed install discarded evidence after rollback launchctl probe failure" >&2
  exit 1
fi
rm -f "$tmp_dir/print-fail-armed"
recover_or_fail "rollback launchctl probe failure"
assert_old_transaction
assert_transaction_clean

reset_transaction
set +e
printf 'new-token\n' | run_transaction cleanup-kill >/dev/null 2>&1
cleanup_kill_status=$?
set -e
if [[ $cleanup_kill_status -eq 0 || -e "$transaction_home/Library/Application Support/KBase/.managed-worker-install-journal" ||
  ! -f "$transaction_home/Library/Application Support/KBase/.managed-worker-install-plist-old" || ! -f "$keychain_dir/backup" ]]; then
  echo "managed install rollback cleanup did not durably remove the journal before evidence" >&2
  exit 1
fi
recover_or_fail "rollback-cleanup SIGKILL"
assert_old_transaction
assert_transaction_clean

reset_transaction
rm -f "$keychain_dir/main"
printf 'unloaded\n' >"$service_state"
unset term_status
set +e
printf 'new-token\n' | run_transaction term >/dev/null 2>&1
term_status=$?
set -e
if [[ $term_status -ne 143 || -e "$keychain_dir/main" || "$(<"$service_state")" != unloaded ]]; then
  echo "managed install did not restore missing-token/unloaded state" >&2
  exit 1
fi
[[ "$(<"$transaction_worker")" == old-worker && "$(<"$transaction_updater")" == old-updater && "$(<"$transaction_plist")" == old-plist ]]
assert_transaction_clean

reset_transaction
rm -f "$tmp_dir/rollback-fail-armed"
set +e
rollback_output="$(printf 'new-token\n' | run_transaction rollback-fail 2>&1)"
rollback_status=$?
set -e
if [[ $rollback_status -ne 97 || "$rollback_output" != "managed worker installation rollback failed" ]]; then
  echo "managed install rollback failure was not fixed and secret-free: status=$rollback_status output=$rollback_output" >&2
  exit 1
fi
if [[ ! -f "$transaction_home/Library/Application Support/KBase/.managed-worker-install-journal" || ! -f "$keychain_dir/backup" ]]; then
  echo "managed install rollback failure discarded recovery evidence" >&2
  exit 1
fi
rm -f "$tmp_dir/rollback-fail-armed"
recover_or_fail "rollback-failure"
assert_old_transaction
assert_transaction_clean

reset_transaction
order_file="$tmp_dir/install-order"
ORDER_FILE="$order_file" run_transaction hold >/dev/null 2>&1 &
holder_pid=$!
for ((attempt = 0; attempt < 100; attempt++)); do
  [[ -f "$order_file" ]] && break
  sleep 0.02
done
ORDER_FILE="$order_file" run_transaction wait >/dev/null 2>&1 &
waiter_pid=$!
wait "$holder_pid"
wait "$waiter_pid"
if [[ "$(sed -n '1p' "$order_file")" != holder-acquired || "$(sed -n '2p' "$order_file")" != holder-released ||
  "$(sed -n '3p' "$order_file")" != waiter-acquired ]]; then
  echo "managed install shared-account lock did not serialize installers" >&2
  exit 1
fi
assert_old_transaction
assert_transaction_clean

echo "managed worker install smoke passed"
