#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/managed-worker-uninstall.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

home="$tmp_dir/home"
install_dir="$home/bin"
plist_dir="$home/Library/LaunchAgents"
mkdir -p "$install_dir/.source-agent-staging" "$install_dir/.source-agent-handoff" "$plist_dir" "$home/keychain"
chmod 0700 "$install_dir"
home="$(cd "$home" && pwd -P)"
install_dir="$home/bin"
plist_dir="$home/Library/LaunchAgents"
worker="$install_dir/source-agent"
updater="$install_dir/source-agent-updater"
config="$install_dir/.source-agent-updater-config.json"
plist="$plist_dir/life.executor.kbase.source-agent.plist"
updater_plist="$plist_dir/life.executor.kbase.source-agent.updater.plist"
worker_state="$home/worker-state"
updater_state="$home/updater-state"
token="$home/keychain/shared-token"
updater_fixture="$tmp_dir/source-agent-updater"
go build -o "$updater_fixture" "$script_dir/../cmd/source-agent-updater"

# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$script_dir/lib/managed-worker-pair.sh"
# shellcheck source=scripts/lib/managed-worker-install.sh
source "$script_dir/lib/managed-worker-install.sh"
# shellcheck source=scripts/lib/managed-worker-uninstall.sh
source "$script_dir/lib/managed-worker-uninstall.sh"

_managed_worker_install_launchctl() {
  local operation="$1" target state
  shift
  target="${1:?}"
  if [[ "$operation" == bootstrap ]]; then target="${2:?}"; fi
  case "$target" in *updater*) state="$updater_state" ;; *) state="$worker_state" ;; esac
  case "$operation" in
    print) [[ -f "$state" && "$(<"$state")" == loaded ]] || return 113 ;;
    bootout)
      if [[ "${FAIL_WORKER_BOOTOUT:-false}" == true && "$state" == "$worker_state" ]]; then return 88; fi
      printf 'unloaded\n' >"$state"
      ;;
    bootstrap) printf 'loaded\n' >"$state" ;;
    kickstart) ;;
    *) return 64 ;;
  esac
}

reset_fixture() {
  printf old-worker >"$worker"
  cp "$updater_fixture" "$updater"
  chmod 0755 "$updater"
  printf old-config >"$config"
  printf old-plist >"$plist"
  printf old-updater-plist >"$updater_plist"
  printf loaded >"$worker_state"
  printf loaded >"$updater_state"
  printf shared-token >"$token"
  mkdir -p "$install_dir/.source-agent-staging" "$install_dir/.source-agent-handoff"
}

run_uninstall() {
  managed_worker_uninstall_run "$home" source-agent "$worker" "$updater" "$plist" "$updater_plist" "$config" \
    gui/501 life.executor.kbase.source-agent life.executor.kbase.source-agent.updater wechat-worker
}

reset_fixture
FAIL_WORKER_BOOTOUT=true
export FAIL_WORKER_BOOTOUT
set +e
run_uninstall >/dev/null 2>&1
failure_status=$?
set -e
unset FAIL_WORKER_BOOTOUT
if [[ $failure_status -eq 0 || "$(<"$worker_state")" != loaded || "$(<"$updater_state")" != loaded ]]; then
  echo "uninstall bootout failure did not restore both prior services" >&2
  exit 1
fi
for path in "$worker" "$updater" "$config" "$plist" "$updater_plist" "$token"; do
  [[ -f "$path" ]] || { echo "uninstall bootout failure removed protected state" >&2; exit 1; }
done

_managed_worker_uninstall_after_phase() {
  if [[ "$1" == stopped ]]; then return 99; fi
}

reset_fixture
set +e
run_uninstall >/dev/null 2>&1
crash_status=$?
set -e
if [[ $crash_status -eq 0 ]]; then
  echo "uninstall crash fixture unexpectedly succeeded" >&2
  exit 1
fi
_managed_worker_uninstall_after_phase() { :; }
set +e
recovery_output="$(run_uninstall 2>&1)"
recovery_status=$?
set -e
if [[ $recovery_status -ne 0 ]]; then
  echo "uninstall recovery exited $recovery_status" >&2
  printf '%s\n' "$recovery_output" >&2
  exit 1
fi
for path in "$worker" "$updater" "$config" "$plist" "$updater_plist"; do
  [[ ! -e "$path" ]] || { echo "uninstall recovery left managed file" >&2; exit 1; }
done
if [[ ! -f "$token" || "$(<"$token")" != shared-token ]]; then
  echo "uninstall changed the shared transport token" >&2
  exit 1
fi
if [[ -e "$home/Library/Application Support/KBase/.managed-worker-uninstall-source-agent-journal" ]]; then
  echo "uninstall recovery left its journal" >&2
  exit 1
fi

echo "managed worker uninstall smoke passed"
