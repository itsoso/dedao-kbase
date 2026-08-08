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

purge=false
if [[ "${1:-}" == --purge-state ]]; then
  purge=true
elif [[ $# -gt 0 ]]; then
  echo "usage: uninstall-source-agent-macos.sh [--purge-state]" >&2
  exit 2
fi
if [[ "$(uname -s)" != Darwin ]]; then
  echo "source-agent uninstallation requires macOS" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$script_dir/lib/managed-worker-pair.sh"
# shellcheck source=scripts/lib/managed-worker-install.sh
source "$script_dir/lib/managed-worker-install.sh"
# shellcheck source=scripts/lib/managed-worker-uninstall.sh
source "$script_dir/lib/managed-worker-uninstall.sh"

home="${HOME:?HOME is required}"
label=life.executor.kbase.source-agent
updater_label=life.executor.kbase.source-agent.updater
install_dir="${SOURCE_AGENT_INSTALL_DIR:-$home/Library/Application Support/KBase/bin}"
state_dir="${SOURCE_AGENT_STATE_DIR:-$home/Library/Application Support/KBase/source-agent}"
plist_path="${SOURCE_AGENT_PLIST_PATH:-$home/Library/LaunchAgents/$label.plist}"
updater_plist_path="${SOURCE_AGENT_UPDATER_PLIST_PATH:-$home/Library/LaunchAgents/$updater_label.plist}"

assert_safe_private_path() {
  local candidate="$1"
  [[ "$candidate" == "$home"/* && "$candidate" != "$home" && "$candidate" != "$home/" ]] || {
    echo "refusing unsafe source-agent path" >&2
    exit 2
  }
}
assert_safe_private_path "$install_dir"
assert_safe_private_path "$state_dir"

if [[ ! -d "$install_dir" ]]; then
  if [[ -e "$plist_path" || -e "$updater_plist_path" ]]; then
    echo "source-agent install directory is missing while LaunchAgent state remains" >&2
    exit 1
  fi
else
  install_dir="$(cd "$install_dir" && pwd -P)"
  plist_dir="$(cd "$(dirname "$plist_path")" && pwd -P)"
  plist_path="$plist_dir/$(basename "$plist_path")"
  updater_plist_path="$plist_dir/$(basename "$updater_plist_path")"
  worker="$install_dir/source-agent"
  updater="$install_dir/source-agent-updater"
  config="$install_dir/.source-agent-updater-config.json"
  if [[ -x "$updater" && -d "$install_dir/.source-agent-staging" && -d "$install_dir/.source-agent-handoff" ]]; then
    "$updater" --check-uninstall --worker-type wechat-worker >/dev/null 2>&1 || {
      echo "source-agent has unresolved update state; repair it before uninstall" >&2
      exit 1
    }
  fi
  managed_worker_uninstall_run "$home" source-agent "$worker" "$updater" \
    "$plist_path" "$updater_plist_path" "$config" "gui/$(id -u)" "$label" "$updater_label" wechat-worker
fi

if [[ "$purge" == true ]]; then
  rm -rf "$state_dir"
  echo "State deleted"
else
  echo "State preserved"
fi
echo "source-agent uninstalled; shared transport token preserved"
