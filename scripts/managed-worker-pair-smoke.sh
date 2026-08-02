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
  if compgen -G "$pair_dir/.source-agent.pair-*" >/dev/null; then
    echo "managed pair transaction left debris" >&2
    find "$pair_dir" -maxdepth 1 -name '.source-agent.pair-*' -print >&2
    exit 1
  fi
}

publish_and_commit() {
  managed_worker_pair_publish "$worker_new" "$updater_new" "$worker" "$updater"
  managed_worker_pair_commit
}

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
journal="$pair_dir/.source-agent.pair-journal"
if grep -Fq 'new-worker-secret-sentinel' "$journal" || grep -Eq '(^|=)(command|path)=' "$journal"; then
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
export FAIL_RM_MATCH='.source-agent.pair-worker-old'
if managed_worker_pair_commit; then
  echo "managed pair ignored commit removal failure" >&2
  exit 1
fi
clear_faults
managed_worker_pair_recover "$worker" "$updater"
assert_new_pair
assert_clean

reset_pair
printf 'version=1\nphase=prepared\n%s\n' "$(printf 'x%.0s' {1..2048})" >"$journal"
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
