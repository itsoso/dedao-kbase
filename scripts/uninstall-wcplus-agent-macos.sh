#!/usr/bin/env bash

set -euo pipefail

label="${WCPLUS_AGENT_LABEL:-life.executor.kbase.wcplus-agent}"
install_dir="${WCPLUS_AGENT_INSTALL_DIR:-$HOME/Library/Application Support/dedao-kbase/bin}"
plist_path="${WCPLUS_AGENT_PLIST_PATH:-$HOME/Library/LaunchAgents/$label.plist}"
state_dir="${WCPLUS_AGENT_STATE_DIR:-}"
log_dir="${WCPLUS_AGENT_LOG_DIR:-$HOME/Library/Logs/dedao-kbase/wcplus-agent}"
mode="uninstall"
delete_state=false
delete_logs=false

usage() {
  echo "usage: uninstall-wcplus-agent-macos.sh [--check] [--delete-state] [--delete-logs]" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check) mode="check" ;;
    --delete-state) delete_state=true ;;
    --delete-logs) delete_logs=true ;;
    *)
      usage
      exit 2
      ;;
  esac
  shift
done

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "wcplus-agent uninstallation requires macOS" >&2
  exit 1
fi
if ! command -v launchctl >/dev/null 2>&1; then
  echo "missing required command: launchctl" >&2
  exit 1
fi
if [[ "$delete_state" == true && -z "$state_dir" ]]; then
  echo "WCPLUS_AGENT_STATE_DIR is required with --delete-state" >&2
  exit 2
fi

assert_safe_delete_path() {
  local candidate="$1"
  case "$candidate" in
    "" | / | . | .. | "$HOME" | "$HOME/")
      echo "refusing unsafe delete path" >&2
      exit 2
      ;;
  esac
  if [[ "$candidate" != /* ]]; then
    echo "delete paths must be absolute" >&2
    exit 2
  fi
}

if [[ "$delete_state" == true ]]; then
  assert_safe_delete_path "$state_dir"
fi
if [[ "$delete_logs" == true ]]; then
  assert_safe_delete_path "$log_dir"
fi

if [[ "$mode" == "check" ]]; then
  echo "wcplus-agent uninstall configuration is valid"
  exit 0
fi

domain="gui/$(id -u)"
if launchctl print "$domain/$label" >/dev/null 2>&1; then
  launchctl bootout "$domain/$label"
fi
rm -f "$plist_path" "$install_dir/wcplus-agent"

if [[ "$delete_state" == true ]]; then
  rm -rf "$state_dir"
  echo "State deleted"
else
  echo "State preserved"
fi
if [[ "$delete_logs" == true ]]; then
  rm -rf "$log_dir"
  echo "Logs deleted"
else
  echo "Logs preserved"
fi
echo "wcplus-agent uninstalled"
