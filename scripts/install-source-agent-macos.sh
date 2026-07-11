#!/bin/bash
set -euo pipefail
: "${KBASE_REMOTE_URL:?KBASE_REMOTE_URL is required}"
: "${KBASE_SOURCE_AGENT_ID:?KBASE_SOURCE_AGENT_ID is required}"
: "${KBASE_SOURCE_AGENT_TOKEN:?KBASE_SOURCE_AGENT_TOKEN is required}"
if [[ -n "${KBASE_AUTH_TOKEN:-}" && "$KBASE_AUTH_TOKEN" == "$KBASE_SOURCE_AGENT_TOKEN" ]]; then
  echo "admin and source-agent tokens must differ" >&2; exit 1
fi
home="${HOME:?HOME is required}"
label="life.executor.kbase.source-agent"
bin_dir="$home/Library/Application Support/KBase/bin"
state_dir="${SOURCE_AGENT_STATE_DIR:-$home/Library/Application Support/KBase/source-agent}"
enroll_addr="${SOURCE_AGENT_ENROLL_ADDR:-127.0.0.1:8765}"
log_dir="$state_dir/logs"
plist="$home/Library/LaunchAgents/$label.plist"
mkdir -p "$bin_dir" "$state_dir" "$log_dir" "$(dirname "$plist")"
chmod 700 "$state_dir" "$log_dir"
SOURCE_AGENT_OUTPUT="$bin_dir/source-agent" "$(dirname "$0")/build-source-agent-macos.sh" >/dev/null
chmod 755 "$bin_dir/source-agent"
printf '%s\n%s\n' "$KBASE_SOURCE_AGENT_TOKEN" "$KBASE_SOURCE_AGENT_TOKEN" | /usr/bin/security add-generic-password -U -s "$label" -a "$KBASE_SOURCE_AGENT_ID:transport-token" -w

temp_plist="$plist.tmp.$$"
trap 'rm -f "$temp_plist"' EXIT
/usr/bin/plutil -create xml1 "$temp_plist"
/usr/bin/plutil -insert Label -string "$label" "$temp_plist"
/usr/bin/plutil -insert ProgramArguments -array "$temp_plist"
/usr/bin/plutil -insert ProgramArguments.0 -string "$bin_dir/source-agent" "$temp_plist"
/usr/bin/plutil -insert ProgramArguments.1 -string run "$temp_plist"
/usr/bin/plutil -insert EnvironmentVariables -dictionary "$temp_plist"
/usr/bin/plutil -insert EnvironmentVariables.KBASE_REMOTE_URL -string "$KBASE_REMOTE_URL" "$temp_plist"
/usr/bin/plutil -insert EnvironmentVariables.KBASE_SOURCE_AGENT_ID -string "$KBASE_SOURCE_AGENT_ID" "$temp_plist"
/usr/bin/plutil -insert EnvironmentVariables.SOURCE_AGENT_STATE_DIR -string "$state_dir" "$temp_plist"
/usr/bin/plutil -insert EnvironmentVariables.SOURCE_AGENT_ENROLL_ADDR -string "$enroll_addr" "$temp_plist"
/usr/bin/plutil -insert RunAtLoad -bool true "$temp_plist"
/usr/bin/plutil -insert KeepAlive -bool true "$temp_plist"
/usr/bin/plutil -insert ProcessType -string Background "$temp_plist"
/usr/bin/plutil -insert ThrottleInterval -integer 15 "$temp_plist"
/usr/bin/plutil -insert StandardOutPath -string "$log_dir/stdout.log" "$temp_plist"
/usr/bin/plutil -insert StandardErrorPath -string "$log_dir/stderr.log" "$temp_plist"
mv "$temp_plist" "$plist"
chmod 600 "$plist"

env -u KBASE_SOURCE_AGENT_TOKEN \
  KBASE_REMOTE_URL="$KBASE_REMOTE_URL" \
  KBASE_SOURCE_AGENT_ID="$KBASE_SOURCE_AGENT_ID" \
  SOURCE_AGENT_STATE_DIR="$state_dir" \
  "$bin_dir/source-agent" doctor >/dev/null

domain="gui/$(id -u)"
launchctl bootout "$domain/$label" 2>/dev/null || true
launchctl bootstrap "$domain" "$plist"
launchctl kickstart -k "$domain/$label"
echo "installed and started; open http://$enroll_addr to scan-login"
