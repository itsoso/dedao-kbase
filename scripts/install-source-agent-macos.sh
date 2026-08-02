#!/bin/bash

set -euo pipefail
set +a
umask 077

mode="install"

usage() {
  echo "usage: install-source-agent-macos.sh [--check|--render-plist]" >&2
}

case "${1:-}" in
  "") ;;
  --check) mode="check" ;;
  --render-plist) mode="render" ;;
  *)
    usage
    exit 2
    ;;
esac

required_names=(KBASE_REMOTE_URL KBASE_SOURCE_AGENT_ID KBASE_SOURCE_AGENT_TOKEN)
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
if [[ -n "${KBASE_AUTH_TOKEN:-}" && "$KBASE_AUTH_TOKEN" == "$KBASE_SOURCE_AGENT_TOKEN" ]]; then
  echo "admin and source-agent tokens must differ" >&2
  exit 2
fi
unset transport_token
transport_token="$KBASE_SOURCE_AGENT_TOKEN"
unset KBASE_SOURCE_AGENT_TOKEN

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
home="${HOME:?HOME is required}"
label="life.executor.kbase.source-agent"
worker_type="wechat-worker"
transport_token_service="life.executor.kbase.source-agent"
transport_token_account="transport-token"
max_transport_token_bytes=1024
binary_source="${SOURCE_AGENT_BINARY_PATH:-$repo_root/build/bin/source-agent}"
updater_source="${SOURCE_AGENT_UPDATER_BINARY_PATH:-$repo_root/build/bin/source-agent-updater}"
install_dir="${SOURCE_AGENT_INSTALL_DIR:-$home/Library/Application Support/KBase/bin}"
state_dir="${SOURCE_AGENT_STATE_DIR:-$home/Library/Application Support/KBase/source-agent}"
log_dir="${SOURCE_AGENT_LOG_DIR:-$state_dir/logs}"
plist_path="${SOURCE_AGENT_PLIST_PATH:-$home/Library/LaunchAgents/$label.plist}"
enroll_addr="${SOURCE_AGENT_ENROLL_ADDR:-127.0.0.1:8765}"

for command_name in cat install launchctl mktemp mv plutil sed; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing required command: $command_name" >&2
    exit 1
  fi
done
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "source-agent installation requires macOS" >&2
  exit 1
fi
if [[ ! -x "$binary_source" ]]; then
  echo "SOURCE_AGENT_BINARY_PATH must point to an executable" >&2
  exit 2
fi
if [[ ! -x "$updater_source" ]]; then
  echo "SOURCE_AGENT_UPDATER_BINARY_PATH must point to an executable" >&2
  exit 2
fi
if ((${#transport_token} > max_transport_token_bytes)) || ! printf '%s' "$transport_token" | LC_ALL=C grep -Eq '^[!-~]+$'; then
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
case "$enroll_addr" in
  127.0.0.1:* | localhost:* | \[::1\]:*) ;;
  *)
    echo "SOURCE_AGENT_ENROLL_ADDR must bind loopback" >&2
    exit 2
    ;;
esac

if ! "$updater_source" --check --worker-type "$worker_type" >/dev/null 2>&1; then
  echo "source-agent updater preflight failed" >&2
  exit 1
fi

xml_escape() {
  printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

render_plist() {
  local destination="$1"
  local installed_binary="$2"
  local label_xml binary_xml remote_xml agent_id_xml state_xml enroll_xml stdout_xml stderr_xml
  label_xml="$(xml_escape "$label")"
  binary_xml="$(xml_escape "$installed_binary")"
  remote_xml="$(xml_escape "$KBASE_REMOTE_URL")"
  agent_id_xml="$(xml_escape "$KBASE_SOURCE_AGENT_ID")"
  state_xml="$(xml_escape "$state_dir")"
  enroll_xml="$(xml_escape "$enroll_addr")"
  stdout_xml="$(xml_escape "$log_dir/stdout.log")"
  stderr_xml="$(xml_escape "$log_dir/stderr.log")"
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
    <key>SOURCE_AGENT_STATE_DIR</key>
    <string>$state_xml</string>
    <key>SOURCE_AGENT_ENROLL_ADDR</key>
    <string>$enroll_xml</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>15</integer>
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

tmp_plist="$(mktemp "${TMPDIR:-/tmp}/source-agent.XXXXXX.plist")"
worker_tmp=""
updater_tmp=""
plist_tmp=""
cleanup() {
  rm -f "$tmp_plist"
  [[ -z "$worker_tmp" ]] || rm -f "$worker_tmp"
  [[ -z "$updater_tmp" ]] || rm -f "$updater_tmp"
  [[ -z "$plist_tmp" ]] || rm -f "$plist_tmp"
}
trap cleanup EXIT

publish_artifact_pair() {
  local worker_source="$1" updater_source="$2" worker_destination="$3" updater_destination="$4"
  local worker_backup="${worker_destination}.backup.$$" updater_backup="${updater_destination}.backup.$$"
  local worker_backed_up=false updater_backed_up=false worker_published=false updater_published=false
  local rollback_failed=false destination

  for destination in "$worker_destination" "$updater_destination"; do
    if [[ -L "$destination" || (-e "$destination" && ! -f "$destination") ]]; then
      echo "artifact destination must be a regular file" >&2
      return 1
    fi
  done
  if [[ -e "$worker_backup" || -L "$worker_backup" || -e "$updater_backup" || -L "$updater_backup" ]]; then
    echo "artifact publication backup already exists" >&2
    return 1
  fi
  if [[ -e "$worker_destination" ]]; then
    if ! mv -f "$worker_destination" "$worker_backup"; then return 1; fi
    worker_backed_up=true
  fi
  if [[ -e "$updater_destination" ]]; then
    if ! mv -f "$updater_destination" "$updater_backup"; then
      if [[ "$worker_backed_up" == true ]] && ! mv -f "$worker_backup" "$worker_destination"; then rollback_failed=true; fi
      [[ "$rollback_failed" == false ]] || echo "artifact publication rollback failed" >&2
      return 1
    fi
    updater_backed_up=true
  fi
  if mv -f "$worker_source" "$worker_destination"; then
    worker_published=true
    if mv -f "$updater_source" "$updater_destination"; then updater_published=true; fi
  fi
  if [[ "$worker_published" == true && "$updater_published" == true ]]; then
    rm -f "$worker_backup" "$updater_backup"
    return 0
  fi

  if [[ "$worker_published" == true ]] && ! rm -f "$worker_destination"; then rollback_failed=true; fi
  if [[ "$updater_published" == true ]] && ! rm -f "$updater_destination"; then rollback_failed=true; fi
  if [[ "$worker_backed_up" == true ]] && ! mv -f "$worker_backup" "$worker_destination"; then rollback_failed=true; fi
  if [[ "$updater_backed_up" == true ]] && ! mv -f "$updater_backup" "$updater_destination"; then rollback_failed=true; fi
  if [[ "$rollback_failed" == true ]]; then echo "artifact publication rollback failed" >&2; fi
  return 1
}

if [[ "$mode" == "render" ]]; then
  render_plist "$tmp_plist" "$binary_source"
  cat "$tmp_plist"
  exit 0
fi
if [[ "$mode" == "check" ]]; then
  render_plist "$tmp_plist" "$binary_source"
  echo "source-agent installation configuration is valid"
  exit 0
fi

mkdir -p "$install_dir" "$state_dir" "$log_dir" "$(dirname "$plist_path")"
chmod 0700 "$install_dir" "$state_dir" "$log_dir"
binary_source="$(cd "$(dirname "$binary_source")" && pwd -P)/$(basename "$binary_source")"
updater_source="$(cd "$(dirname "$updater_source")" && pwd -P)/$(basename "$updater_source")"
install_dir="$(cd "$install_dir" && pwd -P)"
state_dir="$(cd "$state_dir" && pwd -P)"
log_dir="$(cd "$log_dir" && pwd -P)"
plist_dir="$(cd "$(dirname "$plist_path")" && pwd -P)"
plist_path="$plist_dir/$(basename "$plist_path")"
installed_worker="$install_dir/source-agent"
installed_updater="$install_dir/source-agent-updater"

worker_tmp="$install_dir/.source-agent.$$"
updater_tmp="$install_dir/.source-agent-updater.$$"
install -m 0755 "$binary_source" "$worker_tmp"
install -m 0755 "$updater_source" "$updater_tmp"
if ! publish_artifact_pair "$worker_tmp" "$updater_tmp" "$installed_worker" "$installed_updater"; then
  echo "source-agent artifact installation failed" >&2
  exit 1
fi
worker_tmp=""
updater_tmp=""
if ! "$install_dir/source-agent-updater" --check --worker-type wechat-worker >/dev/null 2>&1; then
  echo "installed source-agent updater preflight failed" >&2
  exit 1
fi

if ! printf '%s\n%s\n' "$transport_token" "$transport_token" | \
  /usr/bin/security add-generic-password -U -s "$transport_token_service" -a "$transport_token_account" -w >/dev/null 2>&1; then
  echo "store source-agent transport token failed" >&2
  exit 1
fi

render_plist "$tmp_plist" "$installed_worker"
plist_tmp="$plist_path.tmp.$$"
install -m 0600 "$tmp_plist" "$plist_tmp"
mv -f "$plist_tmp" "$plist_path"
plist_tmp=""

env -u KBASE_SOURCE_AGENT_TOKEN \
  KBASE_REMOTE_URL="$KBASE_REMOTE_URL" \
  KBASE_SOURCE_AGENT_ID="$KBASE_SOURCE_AGENT_ID" \
  SOURCE_AGENT_STATE_DIR="$state_dir" \
  "$installed_worker" doctor >/dev/null

domain="gui/$(id -u)"
if launchctl print "$domain/$label" >/dev/null 2>&1; then
  launchctl bootout "$domain/$label"
fi
launchctl bootstrap "$domain" "$plist_path"
launchctl kickstart -k "$domain/$label"
echo "source-agent installed and started; open http://$enroll_addr to scan-login"
