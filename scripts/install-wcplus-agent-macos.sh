#!/usr/bin/env bash

set -euo pipefail
umask 077

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
label="${WCPLUS_AGENT_LABEL:-life.executor.kbase.wcplus-agent}"
binary_source="${WCPLUS_AGENT_BINARY_PATH:-$repo_root/build/bin/wcplus-agent}"
install_dir="${WCPLUS_AGENT_INSTALL_DIR:-$HOME/Library/Application Support/dedao-kbase/bin}"
plist_path="${WCPLUS_AGENT_PLIST_PATH:-$HOME/Library/LaunchAgents/$label.plist}"
state_dir="${WCPLUS_AGENT_STATE_DIR:-}"
log_dir="${WCPLUS_AGENT_LOG_DIR:-$HOME/Library/Logs/dedao-kbase/wcplus-agent}"
poll_seconds="${WCPLUS_AGENT_POLL_SECONDS:-15}"
restart_seconds="${WCPLUS_AGENT_RESTART_SECONDS:-30}"
wcplus_url="${WCPLUSPRO_BASE_URL:-${WCPLUS_BASE_URL:-http://127.0.0.1:5001}}"
mode="install"

usage() {
  echo "usage: install-wcplus-agent-macos.sh [--check]" >&2
}

case "${1:-}" in
  "") ;;
  --check) mode="check" ;;
  *)
    usage
    exit 2
    ;;
esac

required_names=(
  KBASE_REMOTE_URL
  KBASE_SOURCE_AGENT_ID
  KBASE_SOURCE_AGENT_TOKEN
  WCPLUS_AGENT_STATE_DIR
)
missing_names=()
for name in "${required_names[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    missing_names+=("$name")
  fi
done
if [[ ${#missing_names[@]} -gt 0 ]]; then
  echo "missing required environment variables:" >&2
  printf '  %s\n' "${missing_names[@]}" >&2
  exit 2
fi

for command_name in grep install launchctl mktemp plutil sed; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing required command: $command_name" >&2
    exit 1
  fi
done
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "wcplus-agent installation requires macOS" >&2
  exit 1
fi
if [[ ! -x "$binary_source" ]]; then
  echo "WCPLUS_AGENT_BINARY_PATH must point to an executable" >&2
  exit 2
fi
if ! printf '%s' "$KBASE_SOURCE_AGENT_TOKEN" | LC_ALL=C grep -Eq '^[!-~]+$'; then
  echo "KBASE_SOURCE_AGENT_TOKEN must contain printable ASCII without spaces" >&2
  exit 2
fi
case "$KBASE_REMOTE_URL" in
  https://* | http://127.0.0.1 | http://127.0.0.1:* | http://localhost | http://localhost:* | http://\[::1\] | http://\[::1\]:*) ;;
  *)
    echo "KBASE_REMOTE_URL must use HTTPS unless it targets loopback" >&2
    exit 2
    ;;
esac
case "$wcplus_url" in
  http://127.0.0.1 | http://127.0.0.1:* | http://localhost | http://localhost:* | http://\[::1\] | http://\[::1\]:* | https://127.0.0.1 | https://127.0.0.1:* | https://localhost | https://localhost:* | https://\[::1\] | https://\[::1\]:*) ;;
  *)
    echo "WCPLUSPRO_BASE_URL must target loopback" >&2
    exit 2
    ;;
esac
if [[ ! "$poll_seconds" =~ ^[0-9]+$ ]] || ((poll_seconds < 1 || poll_seconds > 300)); then
  echo "WCPLUS_AGENT_POLL_SECONDS must be between 1 and 300" >&2
  exit 2
fi
if [[ ! "$restart_seconds" =~ ^[0-9]+$ ]] || ((restart_seconds < 10 || restart_seconds > 300)); then
  echo "WCPLUS_AGENT_RESTART_SECONDS must be between 10 and 300" >&2
  exit 2
fi

xml_escape() {
  printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

render_plist() {
  local destination="$1"
  local installed_binary="$2"
  local resolved_state_dir="$3"
  local resolved_log_dir="$4"
  local label_xml binary_xml remote_xml agent_id_xml token_xml wcplus_xml state_xml stdout_xml stderr_xml
  label_xml="$(xml_escape "$label")"
  binary_xml="$(xml_escape "$installed_binary")"
  remote_xml="$(xml_escape "$KBASE_REMOTE_URL")"
  agent_id_xml="$(xml_escape "$KBASE_SOURCE_AGENT_ID")"
  token_xml="$(xml_escape "$KBASE_SOURCE_AGENT_TOKEN")"
  wcplus_xml="$(xml_escape "$wcplus_url")"
  state_xml="$(xml_escape "$resolved_state_dir")"
  stdout_xml="$(xml_escape "$resolved_log_dir/stdout.log")"
  stderr_xml="$(xml_escape "$resolved_log_dir/stderr.log")"
  cat >"$destination" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label_xml</string>
  <key>ProgramArguments</key>
  <array>
    <string>$binary_xml</string>
    <string>run</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>KBASE_REMOTE_URL</key>
    <string>$remote_xml</string>
    <key>KBASE_SOURCE_AGENT_ID</key>
    <string>$agent_id_xml</string>
    <key>KBASE_SOURCE_AGENT_TOKEN</key>
    <string>$token_xml</string>
    <key>WCPLUSPRO_BASE_URL</key>
    <string>$wcplus_xml</string>
    <key>WCPLUS_AGENT_STATE_DIR</key>
    <string>$state_xml</string>
    <key>WCPLUS_AGENT_POLL_SECONDS</key>
    <string>$poll_seconds</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>$restart_seconds</integer>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>$stdout_xml</string>
  <key>StandardErrorPath</key>
  <string>$stderr_xml</string>
</dict>
</plist>
PLIST
  plutil -lint "$destination" >/dev/null
}

tmp_plist="$(mktemp "${TMPDIR:-/tmp}/wcplus-agent.XXXXXX.plist")"
tmp_binary=""
cleanup() {
  rm -f "$tmp_plist"
  if [[ -n "$tmp_binary" ]]; then
    rm -f "$tmp_binary"
  fi
}
trap cleanup EXIT

if [[ "$mode" == "check" ]]; then
  render_plist "$tmp_plist" "$binary_source" "$state_dir" "$log_dir"
  echo "wcplus-agent installation configuration is valid"
  exit 0
fi

mkdir -p "$install_dir" "$state_dir" "$log_dir" "$(dirname "$plist_path")"
binary_source="$(cd "$(dirname "$binary_source")" && pwd -P)/$(basename "$binary_source")"
install_dir="$(cd "$install_dir" && pwd -P)"
state_dir="$(cd "$state_dir" && pwd -P)"
log_dir="$(cd "$log_dir" && pwd -P)"
plist_dir="$(cd "$(dirname "$plist_path")" && pwd -P)"
plist_path="$plist_dir/$(basename "$plist_path")"
installed_binary="$install_dir/wcplus-agent"

render_plist "$tmp_plist" "$installed_binary" "$state_dir" "$log_dir"
tmp_binary="$install_dir/.wcplus-agent.$$"
install -m 0755 "$binary_source" "$tmp_binary"
mv -f "$tmp_binary" "$installed_binary"
tmp_binary=""

domain="gui/$(id -u)"
if launchctl print "$domain/$label" >/dev/null 2>&1; then
  launchctl bootout "$domain/$label"
fi
install -m 0600 "$tmp_plist" "$plist_path"
launchctl bootstrap "$domain" "$plist_path"
launchctl kickstart -k "$domain/$label"

echo "wcplus-agent installed and started"
echo "LaunchAgent label: $label"
