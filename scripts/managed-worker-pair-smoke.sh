#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
library="$script_dir/lib/managed-worker-pair.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/managed-worker-pair.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

# The library must parse cleanly before any fixture can mutate a pair.
bash -n "$library"
# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$library"

real_path="$PATH"
fake_bin="$tmp_dir/fake-bin"
mkdir -p "$fake_bin"
for command_name in mv cp rm sync; do
  real_command="$(command -v "$command_name")"
  cat >"$fake_bin/$command_name" <<'WRAPPER'
#!/bin/bash
set -euo pipefail
name="${0##*/}"
case "$name" in
  mv)
    destination=""
    for destination in "$@"; do :; done
    if [[ -n "${FAIL_MV_DEST:-}" && "$destination" == "$FAIL_MV_DEST" ]]; then
      count=0
      [[ ! -f "${FAULT_COUNT_FILE:?}" ]] || read -r count <"$FAULT_COUNT_FILE"
      count=$((count + 1))
      printf '%s\n' "$count" >"$FAULT_COUNT_FILE"
      if [[ "$count" == "${FAIL_MV_OCCURRENCE:-1}" ]]; then exit 71; fi
    fi
    exec "${REAL_MV:?}" "$@"
    ;;
  cp)
    if [[ -n "${FAIL_CP_CALL:-}" ]]; then
      count=0
      [[ ! -f "${FAULT_COUNT_FILE:?}" ]] || read -r count <"$FAULT_COUNT_FILE"
      count=$((count + 1))
      printf '%s\n' "$count" >"$FAULT_COUNT_FILE"
      if [[ "$count" == "$FAIL_CP_CALL" ]]; then exit 72; fi
    fi
    exec "${REAL_CP:?}" "$@"
    ;;
  rm)
    for argument in "$@"; do
      if [[ -n "${FAIL_RM_MATCH:-}" && "$argument" == *"$FAIL_RM_MATCH"* ]]; then exit 73; fi
    done
    exec "${REAL_RM:?}" "$@"
    ;;
  sync)
    count=0
    [[ ! -f "${SYNC_COUNT_FILE:?}" ]] || read -r count <"$SYNC_COUNT_FILE"
    count=$((count + 1))
    printf '%s\n' "$count" >"$SYNC_COUNT_FILE"
    if [[ -n "${FAIL_SYNC_CALL:-}" && "$count" == "$FAIL_SYNC_CALL" ]]; then exit 74; fi
    exec "${REAL_SYNC:?}" "$@"
    ;;
esac
WRAPPER
  chmod 0755 "$fake_bin/$command_name"
  case "$command_name" in
    mv) export REAL_MV="$real_command" ;;
    cp) export REAL_CP="$real_command" ;;
    rm) export REAL_RM="$real_command" ;;
    sync) export REAL_SYNC="$real_command" ;;
  esac
done
export PATH="$fake_bin:$real_path"
export FAULT_COUNT_FILE="$tmp_dir/fault-count"
export SYNC_COUNT_FILE="$tmp_dir/sync-count"

pair_dir="$tmp_dir/pair"
worker="$pair_dir/source-agent"
updater="$pair_dir/source-agent-updater"
worker_new="$pair_dir/.source-agent.new"
updater_new="$pair_dir/.source-agent-updater.new"

clear_faults() {
  unset FAIL_MV_DEST FAIL_MV_OCCURRENCE FAIL_CP_CALL FAIL_RM_MATCH FAIL_SYNC_CALL
  "$REAL_RM" -f "$FAULT_COUNT_FILE" "$SYNC_COUNT_FILE"
}

reset_pair() {
  clear_faults
  "$REAL_RM" -rf "$pair_dir"
  mkdir -p "$pair_dir"
  printf 'old-worker' >"$worker"
  printf 'old-updater' >"$updater"
  printf 'new-worker-secret-sentinel' >"$worker_new"
  printf 'new-updater' >"$updater_new"
}

assert_old_pair() {
  [[ -f "$worker" && "$(<"$worker")" == "old-worker" ]]
  [[ -f "$updater" && "$(<"$updater")" == "old-updater" ]]
}

assert_new_pair() {
  [[ -f "$worker" && "$(<"$worker")" == "new-worker-secret-sentinel" ]]
  [[ -f "$updater" && "$(<"$updater")" == "new-updater" ]]
}

assert_clean() {
  if [[ -e "$pair_dir/.source-agent-updater.pair-journal" || -e "$pair_dir/.source-agent-updater.pair-journal.tmp" ||
    -e "$pair_dir/.source-agent-updater.pair-worker-old" || -e "$pair_dir/.source-agent-updater.pair-updater-old" ]] ||
    compgen -G "$pair_dir/.source-agent.pair-*" >/dev/null ||
    compgen -G "$pair_dir/.wcplus-agent.pair-*" >/dev/null ||
    compgen -G "$pair_dir/.source-agent-updater.pair-lock-ready.*" >/dev/null ||
    compgen -G "$pair_dir/.source-agent-updater.pair-lock-release.*" >/dev/null; then
    echo "managed pair transaction left debris" >&2
    find "$pair_dir" -maxdepth 1 -name '*.pair-*' -print >&2
    exit 1
  fi
}

publish_and_commit() {
  managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"
  managed_worker_pair_commit
}

reset_pair
_managed_worker_pair_set_paths "$worker" "$updater"
printf '  999999  \n' >"$MANAGED_WORKER_PAIR_LOCK"
if ! _managed_worker_pair_reclaim_stale_lock || [[ ! -f "$MANAGED_WORKER_PAIR_LOCK" ]]; then
  echo "managed pair could not acquire an unlocked advisory lock with malformed old contents" >&2
  exit 1
fi
"$REAL_RM" -f "$MANAGED_WORKER_PAIR_LOCK"
printf '999999\n' >"$MANAGED_WORKER_PAIR_LOCK"
if ! _managed_worker_pair_reclaim_stale_lock || [[ ! -f "$MANAGED_WORKER_PAIR_LOCK" ]]; then
  echo "managed pair could not acquire an unlocked advisory lock" >&2
  exit 1
fi
"$REAL_RM" -f "$MANAGED_WORKER_PAIR_LOCK"

PAIR_LOCK="$MANAGED_WORKER_PAIR_LOCK" PAIR_LOCK_READY="$tmp_dir/pair-lock-ready" /bin/bash -c '
  exec 7>>"$PAIR_LOCK"
  /usr/bin/lockf -s -t 0 7
  : >"$PAIR_LOCK_READY"
  sleep 5
' &
live_pair_lock_pid=$!
for ((attempt = 0; attempt < 100; attempt++)); do
  [[ -e "$tmp_dir/pair-lock-ready" ]] && break
  sleep 0.01
done
if _managed_worker_pair_reclaim_stale_lock || [[ ! -f "$MANAGED_WORKER_PAIR_LOCK" ]]; then
  kill "$live_pair_lock_pid" 2>/dev/null || true
  echo "managed pair reclaimed a live owner" >&2
  exit 1
fi
kill "$live_pair_lock_pid"
wait "$live_pair_lock_pid" 2>/dev/null || true
"$REAL_RM" -f "$MANAGED_WORKER_PAIR_LOCK"

printf 'stale\n' >"$pair_dir/.source-agent-updater.pair-lock-ready.999999"
printf 'stale\n' >"$pair_dir/.source-agent-updater.pair-lock-release.999999"
if ! _managed_worker_pair_try_acquire_lock ||
  [[ -e "$pair_dir/.source-agent-updater.pair-lock-ready.999999" || -e "$pair_dir/.source-agent-updater.pair-lock-release.999999" ]]; then
  echo "managed pair did not clean power-loss lock-helper markers" >&2
  exit 1
fi
_managed_worker_pair_release_lock

ln -s "$tmp_dir/lock-target" "$pair_dir/.source-agent-updater.pair-lock-ready.999998"
if _managed_worker_pair_try_acquire_lock || [[ ! -L "$pair_dir/.source-agent-updater.pair-lock-ready.999998" ]]; then
  echo "managed pair did not fail closed on a symlink lock-helper marker" >&2
  exit 1
fi
"$REAL_RM" -f "$pair_dir/.source-agent-updater.pair-lock-ready.999998"

_managed_worker_pair_try_acquire_lock
kill -TERM "$MANAGED_WORKER_PAIR_LOCK_HELPER_PID"
sleep 0.1
if /usr/bin/lockf -s -t 0 -k "$MANAGED_WORKER_PAIR_LOCK" /usr/bin/true; then
  echo "managed pair helper released the lock on a cooperative group signal" >&2
  exit 1
fi
_managed_worker_pair_release_lock

pair_acquire_script="$tmp_dir/pair-acquire.sh"
cat >"$pair_acquire_script" <<'PAIR_ACQUIRE'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
_managed_worker_pair_set_paths "${WORKER_DEST:?}" "${UPDATER_DEST:?}"
if _managed_worker_pair_try_acquire_lock; then
  printf '%s\n' "$$" >>"${WINNERS:?}"
  sleep 1
  _managed_worker_pair_release_lock
fi
PAIR_ACQUIRE
chmod 0755 "$pair_acquire_script"
pair_winners="$tmp_dir/pair-winners"
printf '999999\n' >"$MANAGED_WORKER_PAIR_LOCK"
PAIR_LIBRARY="$library" WORKER_DEST="$worker" UPDATER_DEST="$updater" WINNERS="$pair_winners" "$pair_acquire_script" &
first_pair_pid=$!
PAIR_LIBRARY="$library" WORKER_DEST="$worker" UPDATER_DEST="$updater" WINNERS="$pair_winners" "$pair_acquire_script" &
second_pair_pid=$!
wait "$first_pair_pid"
wait "$second_pair_pid"
pair_winner_count="$(wc -l <"$pair_winners")"
pair_winner_count="${pair_winner_count//[[:space:]]/}"
if [[ "$pair_winner_count" != 1 ]]; then
  echo "managed pair atomic lock admitted $pair_winner_count simultaneous owners" >&2
  exit 1
fi

pair_inherit_script="$tmp_dir/pair-inherit.sh"
cat >"$pair_inherit_script" <<'PAIR_INHERIT'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
_managed_worker_pair_set_paths "${WORKER_DEST:?}" "${UPDATER_DEST:?}"
_managed_worker_pair_try_acquire_lock
sleep 5 &
printf '%s\n' "$!" >"${CHILD_PID_FILE:?}"
: >"${READY_FILE:?}"
wait
PAIR_INHERIT
chmod 0755 "$pair_inherit_script"
pair_supervisor_script="$tmp_dir/pair-supervisor.sh"
cat >"$pair_supervisor_script" <<'PAIR_SUPERVISOR'
#!/bin/bash
set -euo pipefail
"${OWNER_SCRIPT:?}" &
owner_pid=$!
printf '%s\n' "$owner_pid" >"${OWNER_PID_FILE:?}"
while [[ ! -e "${READY_FILE:?}" ]]; do sleep 0.01; done
: >"${SUPERVISOR_STOPPING:?}"
kill -STOP $$
wait "$owner_pid"
PAIR_SUPERVISOR
chmod 0755 "$pair_supervisor_script"
PAIR_LIBRARY="$library" WORKER_DEST="$worker" UPDATER_DEST="$updater" \
  CHILD_PID_FILE="$tmp_dir/pair-inherited-child" READY_FILE="$tmp_dir/pair-inherit-ready" \
  OWNER_SCRIPT="$pair_inherit_script" OWNER_PID_FILE="$tmp_dir/pair-owner-pid" \
  SUPERVISOR_STOPPING="$tmp_dir/pair-supervisor-stopping" "$pair_supervisor_script" &
pair_lock_supervisor=$!
for ((attempt = 0; attempt < 100; attempt++)); do
  [[ -e "$tmp_dir/pair-supervisor-stopping" ]] && break
  sleep 0.01
done
sleep 0.1
pair_lock_parent="$(<"$tmp_dir/pair-owner-pid")"
pair_inherited_child="$(<"$tmp_dir/pair-inherited-child")"
kill -KILL "$pair_lock_parent"
pair_reacquired=false
for ((attempt = 0; attempt < 100; attempt++)); do
  if _managed_worker_pair_try_acquire_lock; then pair_reacquired=true; break; fi
  sleep 0.01
done
if [[ "$pair_reacquired" != true ]]; then
  kill "$pair_inherited_child" 2>/dev/null || true
  kill -CONT "$pair_lock_supervisor" 2>/dev/null || true
  wait "$pair_lock_supervisor" 2>/dev/null || true
  echo "managed pair child inherited the lock after owner SIGKILL" >&2
  exit 1
fi
_managed_worker_pair_release_lock
kill -CONT "$pair_lock_supervisor" 2>/dev/null || true
wait "$pair_lock_supervisor" 2>/dev/null || true
kill "$pair_inherited_child" 2>/dev/null || true

reset_pair
publish_and_commit
assert_new_pair
assert_clean

for missing in worker updater both; do
  reset_pair
  case "$missing" in
    worker) "$REAL_RM" -f "$worker" ;;
    updater) "$REAL_RM" -f "$updater" ;;
    both) "$REAL_RM" -f "$worker" "$updater" ;;
  esac
  publish_and_commit
  assert_new_pair
  assert_clean
done

reset_pair
if managed_worker_pair_publish "$worker_new" "$updater_new" "$tmp_dir/other/source-agent" "$updater"; then
  echo "managed pair accepted different destination directories" >&2
  exit 1
fi
assert_old_pair

reset_pair
if managed_worker_pair_publish "$worker_new" "$updater_new" "$pair_dir/not-a-worker" "$updater"; then
  echo "managed pair accepted an unapproved worker basename" >&2
  exit 1
fi
assert_old_pair

for fail_cp in 1 2; do
  reset_pair
  export FAIL_CP_CALL="$fail_cp"
  if managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"; then
    echo "managed pair ignored backup failure $fail_cp" >&2
    exit 1
  fi
  clear_faults
  assert_old_pair
  managed_worker_pair_recover "$worker" "$updater"
  assert_clean
done

for destination in "$worker" "$updater"; do
  reset_pair
  export FAIL_MV_DEST="$destination"
  if managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"; then
    echo "managed pair ignored publication rename failure" >&2
    exit 1
  fi
  clear_faults
  assert_old_pair
  managed_worker_pair_recover "$worker" "$updater"
  assert_clean
done

reset_pair
export FAIL_MV_DEST="$updater"
export FAIL_MV_OCCURRENCE=1
# Fail the updater publication and then the worker restoration (its second
# destination occurrence), leaving a journal that a later invocation recovers.
cat >"$fake_bin/mv" <<'ROLLBACK_MV'
#!/bin/bash
set -euo pipefail
destination=""
for destination in "$@"; do :; done
if [[ "$destination" == "${UPDATER_DEST:?}" ]]; then exit 75; fi
if [[ "$destination" == "${WORKER_DEST:?}" ]]; then
  count=0
  [[ ! -f "${FAULT_COUNT_FILE:?}" ]] || read -r count <"$FAULT_COUNT_FILE"
  count=$((count + 1))
  printf '%s\n' "$count" >"$FAULT_COUNT_FILE"
  if [[ $count -eq 2 ]]; then exit 76; fi
fi
exec "${REAL_MV:?}" "$@"
ROLLBACK_MV
chmod 0755 "$fake_bin/mv"
export WORKER_DEST="$worker" UPDATER_DEST="$updater"
if managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"; then
  echo "managed pair ignored rollback failure" >&2
  exit 1
fi
cat >"$fake_bin/mv" <<'NORMAL_MV'
#!/bin/bash
set -euo pipefail
exec "${REAL_MV:?}" "$@"
NORMAL_MV
chmod 0755 "$fake_bin/mv"
clear_faults
managed_worker_pair_recover "$worker" "$updater"
assert_old_pair
assert_clean

reset_pair
export FAIL_SYNC_CALL=1
if managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"; then
  echo "managed pair ignored global sync failure" >&2
  exit 1
fi
clear_faults
assert_old_pair
managed_worker_pair_recover "$worker" "$updater"
assert_clean

reset_pair
managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"
journal="$pair_dir/.source-agent-updater.pair-journal"
if grep -Fq 'new-worker-secret-sentinel' "$journal" || grep -Eq '(^|=)(command|path)=' "$journal" ||
  ! grep -Eq '^worker=source-agent$' "$journal" || [[ "$(wc -l <"$journal")" -ne 9 ]]; then
  echo "managed pair journal contains secret data or executable input" >&2
  exit 1
fi
printf 'corrupt-new-worker' >"$worker"
if managed_worker_pair_commit; then
  echo "managed pair committed a hash-mismatched pair" >&2
  exit 1
fi
managed_worker_pair_rollback
assert_old_pair
assert_clean

reset_pair
managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"
export FAIL_RM_MATCH='.source-agent-updater.pair-worker-old'
if managed_worker_pair_commit; then
  echo "managed pair ignored commit removal failure" >&2
  exit 1
fi
clear_faults
managed_worker_pair_recover "$worker" "$updater"
assert_new_pair
assert_clean

reset_pair
printf 'version=2\nphase=prepared\n%s\n' "$(printf 'x%.0s' {1..2048})" >"$journal"
if managed_worker_pair_recover "$worker" "$updater"; then
  echo "managed pair accepted an oversized journal" >&2
  exit 1
fi
assert_old_pair
if managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"; then
  echo "managed pair replaced an oversized recovery journal" >&2
  exit 1
fi
if [[ ! -f "$journal" || "$(wc -c <"$journal")" -le 1024 ]]; then
  echo "managed pair did not preserve a rejected recovery journal" >&2
  exit 1
fi
assert_old_pair
"$REAL_RM" -f "$journal"
assert_clean

reset_pair
signal_script="$tmp_dir/signal.sh"
cat >"$signal_script" <<'SIGNAL_SCRIPT'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
trap 'managed_worker_pair_rollback; exit 143' TERM
managed_worker_pair_publish "${WORKER_NEW:?}" "${UPDATER_NEW:?}" "${WORKER_DEST:?}" "${UPDATER_DEST:?}"
kill -TERM $$
exit 99
SIGNAL_SCRIPT
chmod 0755 "$signal_script"
set +e
PAIR_LIBRARY="$library" WORKER_NEW="$worker_new" UPDATER_NEW="$updater_new" WORKER_DEST="$worker" UPDATER_DEST="$updater" \
  "$signal_script" >/dev/null 2>&1
signal_status=$?
set -e
if [[ $signal_status -ne 143 ]]; then
  echo "managed pair SIGTERM fixture exited $signal_status" >&2
  exit 1
fi
assert_old_pair
assert_clean

reset_pair
kill_script="$tmp_dir/kill.sh"
cat >"$kill_script" <<'KILL_SCRIPT'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
managed_worker_pair_publish "${WORKER_NEW:?}" "${UPDATER_NEW:?}" "${WORKER_DEST:?}" "${UPDATER_DEST:?}"
kill -KILL $$
KILL_SCRIPT
chmod 0755 "$kill_script"
set +e
PAIR_LIBRARY="$library" WORKER_NEW="$worker_new" UPDATER_NEW="$updater_new" WORKER_DEST="$worker" UPDATER_DEST="$updater" \
  "$kill_script" >/dev/null 2>&1
kill_status=$?
set -e
if [[ $kill_status -eq 0 ]]; then
  echo "managed pair SIGKILL fixture unexpectedly succeeded" >&2
  exit 1
fi
managed_worker_pair_recover "$worker" "$updater"
assert_old_pair
assert_clean

reset_pair
set +e
PAIR_LIBRARY="$library" WORKER_NEW="$worker_new" UPDATER_NEW="$updater_new" WORKER_DEST="$worker" UPDATER_DEST="$updater" \
  "$kill_script" >/dev/null 2>&1
kill_status=$?
set -e
if [[ $kill_status -eq 0 ]]; then
  echo "managed pair forward-commit SIGKILL fixture unexpectedly succeeded" >&2
  exit 1
fi
managed_worker_pair_finish_commit "$worker" "$updater"
assert_new_pair
assert_clean

reset_pair
cross_worker_failed=false
wcplus_worker="$pair_dir/wcplus-agent"
printf 'old-wcplus-worker' >"$wcplus_worker"
printf 'source-worker-v1' >"$worker_new"
printf 'shared-updater-v1' >"$updater_new"
set +e
PAIR_LIBRARY="$library" WORKER_NEW="$worker_new" UPDATER_NEW="$updater_new" WORKER_DEST="$worker" UPDATER_DEST="$updater" \
  "$kill_script" >/dev/null 2>&1
cross_kill_status=$?
set -e
if [[ $cross_kill_status -eq 0 ]]; then
  echo "managed pair source cross-worker SIGKILL fixture unexpectedly succeeded" >&2
  exit 1
fi
wcplus_new="$pair_dir/.wcplus-agent.cross-new"
updater_cross_new="$pair_dir/.source-agent-updater.cross-new"
printf 'wcplus-worker-v1' >"$wcplus_new"
printf 'shared-updater-v2' >"$updater_cross_new"
managed_worker_pair_publish "$wcplus_new" "$updater_cross_new" "$wcplus_worker" "$updater"
managed_worker_pair_commit
managed_worker_pair_recover "$worker" "$updater"
if [[ "$(<"$worker")" != old-worker || "$(<"$wcplus_worker")" != wcplus-worker-v1 || "$(<"$updater")" != shared-updater-v2 ]]; then
  echo "managed pair delayed source recovery clobbered a committed WC Plus updater" >&2
  cross_worker_failed=true
fi
assert_clean

reset_pair
printf 'old-wcplus-worker' >"$wcplus_worker"
printf 'wcplus-worker-v1' >"$wcplus_new"
printf 'shared-updater-v1' >"$updater_cross_new"
set +e
PAIR_LIBRARY="$library" WORKER_NEW="$wcplus_new" UPDATER_NEW="$updater_cross_new" WORKER_DEST="$wcplus_worker" UPDATER_DEST="$updater" \
  "$kill_script" >/dev/null 2>&1
cross_kill_status=$?
set -e
if [[ $cross_kill_status -eq 0 ]]; then
  echo "managed pair WC Plus cross-worker SIGKILL fixture unexpectedly succeeded" >&2
  exit 1
fi
printf 'source-worker-v1' >"$worker_new"
printf 'shared-updater-v2' >"$updater_new"
managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"
managed_worker_pair_commit
managed_worker_pair_recover "$wcplus_worker" "$updater"
if [[ "$(<"$worker")" != source-worker-v1 || "$(<"$wcplus_worker")" != old-wcplus-worker || "$(<"$updater")" != shared-updater-v2 ]]; then
  echo "managed pair delayed WC Plus recovery clobbered a committed source updater" >&2
  cross_worker_failed=true
fi
assert_clean
if [[ "$cross_worker_failed" == true ]]; then exit 1; fi

reset_pair
hold_script="$tmp_dir/hold.sh"
cat >"$hold_script" <<'HOLD_SCRIPT'
#!/bin/bash
set -euo pipefail
source "${PAIR_LIBRARY:?}"
trap 'managed_worker_pair_rollback; exit 143' TERM
managed_worker_pair_publish "${WORKER_NEW:?}" "${UPDATER_NEW:?}" "${WORKER_DEST:?}" "${UPDATER_DEST:?}"
: >"${HOLD_MARKER:?}"
while :; do sleep 1; done
HOLD_SCRIPT
chmod 0755 "$hold_script"
hold_marker="$tmp_dir/hold-ready"
PAIR_LIBRARY="$library" WORKER_NEW="$worker_new" UPDATER_NEW="$updater_new" WORKER_DEST="$worker" UPDATER_DEST="$updater" HOLD_MARKER="$hold_marker" \
  "$hold_script" >/dev/null 2>&1 &
hold_pid=$!
for ((attempt = 0; attempt < 50; attempt++)); do
  [[ ! -f "$hold_marker" ]] || break
  sleep 0.1
done
if [[ ! -f "$hold_marker" ]]; then
  kill -TERM "$hold_pid" 2>/dev/null || true
  wait "$hold_pid" 2>/dev/null || true
  echo "managed pair lock holder did not start" >&2
  exit 1
fi
printf 'second-worker' >"$pair_dir/.source-agent.second"
printf 'second-updater' >"$pair_dir/.source-agent-updater.second"
if managed_worker_pair_publish "$pair_dir/.source-agent.second" "$pair_dir/.source-agent-updater.second" "$worker" "$updater"; then
  echo "managed pair lock admitted a concurrent publisher" >&2
  exit 1
fi
printf 'old-wcplus-worker' >"$pair_dir/wcplus-agent"
printf 'second-wcplus-worker' >"$pair_dir/.wcplus-agent.second"
printf 'second-shared-updater' >"$pair_dir/.source-agent-updater.wcplus-second"
if managed_worker_pair_publish \
  "$pair_dir/.wcplus-agent.second" \
  "$pair_dir/.source-agent-updater.wcplus-second" \
  "$pair_dir/wcplus-agent" \
  "$updater"; then
  echo "managed pair updater lock admitted a concurrent cross-worker publisher" >&2
  exit 1
fi
kill -TERM "$hold_pid"
set +e
wait "$hold_pid"
hold_status=$?
set -e
if [[ $hold_status -ne 143 ]]; then
  echo "managed pair lock holder exited $hold_status" >&2
  exit 1
fi
assert_old_pair
assert_clean

echo "managed worker pair smoke passed"
