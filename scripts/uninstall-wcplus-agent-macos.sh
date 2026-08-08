#!/usr/bin/env -S -u BASH_ENV -u ENV -u SHELLOPTS /bin/bash -p
set +x
set -euo pipefail
set +a
umask 077
IFS=$' \t\n'
unset CDPATH BASH_ENV ENV KBASE_AUTH_TOKEN KBASE_SOURCE_AGENT_TOKEN
LC_ALL=C
export LC_ALL
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH

label=life.executor.kbase.wcplus-agent
updater_label=life.executor.kbase.wcplus-agent.updater
install_dir="${WCPLUS_AGENT_INSTALL_DIR:-$HOME/Library/Application Support/dedao-kbase/bin}"
plist_path="${WCPLUS_AGENT_PLIST_PATH:-$HOME/Library/LaunchAgents/$label.plist}"
updater_plist_path="${WCPLUS_AGENT_UPDATER_PLIST_PATH:-$HOME/Library/LaunchAgents/$updater_label.plist}"
state_dir="${WCPLUS_AGENT_STATE_DIR:-}"
log_dir="${WCPLUS_AGENT_LOG_DIR:-$HOME/Library/Logs/dedao-kbase/wcplus-agent}"
mode=uninstall
delete_state=false
delete_logs=false

usage() {
  echo "usage: uninstall-wcplus-agent-macos.sh [--check] [--delete-state] [--delete-logs]" >&2
}
while [[ $# -gt 0 ]]; do
  case "$1" in
    --check) mode=check ;;
    --delete-state) delete_state=true ;;
    --delete-logs) delete_logs=true ;;
    *) usage; exit 2 ;;
  esac
  shift
done
if [[ "$(uname -s)" != Darwin ]]; then
  echo "wcplus-agent uninstallation requires macOS" >&2
  exit 1
fi
if [[ "$delete_state" == true && -z "$state_dir" ]]; then
  echo "WCPLUS_AGENT_STATE_DIR is required with --delete-state" >&2
  exit 2
fi

home="${HOME:?HOME is required}"
assert_safe_private_path() {
  local candidate="$1"
  [[ "$candidate" == "$home"/* && "$candidate" != "$home" && "$candidate" != "$home/" ]] || {
    echo "refusing unsafe WC Plus path" >&2
    exit 2
  }
}
assert_safe_private_path "$install_dir"
if [[ "$delete_state" == true ]]; then assert_safe_private_path "$state_dir"; fi
if [[ "$delete_logs" == true ]]; then assert_safe_private_path "$log_dir"; fi
if [[ "$mode" == check ]]; then
  echo "wcplus-agent uninstall configuration is valid"
  exit 0
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$script_dir/lib/managed-worker-pair.sh"
# shellcheck source=scripts/lib/managed-worker-install.sh
source "$script_dir/lib/managed-worker-install.sh"
# shellcheck source=scripts/lib/managed-worker-uninstall.sh
source "$script_dir/lib/managed-worker-uninstall.sh"

if [[ ! -d "$install_dir" ]]; then
  if [[ -e "$plist_path" || -e "$updater_plist_path" ]]; then
    echo "WC Plus install directory is missing while LaunchAgent state remains" >&2
    exit 1
  fi
else
  install_dir="$(cd "$install_dir" && pwd -P)"
  plist_dir="$(cd "$(dirname "$plist_path")" && pwd -P)"
  plist_path="$plist_dir/$(basename "$plist_path")"
  updater_plist_path="$plist_dir/$(basename "$updater_plist_path")"
  worker="$install_dir/wcplus-agent"
  updater="$install_dir/source-agent-updater"
  config="$install_dir/.source-agent-updater-config.json"
  if [[ -x "$updater" && -d "$install_dir/.source-agent-staging" && -d "$install_dir/.source-agent-handoff" ]]; then
    "$updater" --check-uninstall --worker-type wcplus-worker >/dev/null 2>&1 || {
      echo "WC Plus has unresolved update state; repair it before uninstall" >&2
      exit 1
    }
  fi
  managed_worker_uninstall_run "$home" wcplus-agent "$worker" "$updater" \
    "$plist_path" "$updater_plist_path" "$config" "gui/$(id -u)" "$label" "$updater_label" wcplus-worker
fi

if [[ "$delete_state" == true ]]; then rm -rf "$state_dir"; echo "State deleted"; else echo "State preserved"; fi
if [[ "$delete_logs" == true ]]; then rm -rf "$log_dir"; echo "Logs deleted"; else echo "Logs preserved"; fi
echo "wcplus-agent uninstalled; shared transport token preserved"
