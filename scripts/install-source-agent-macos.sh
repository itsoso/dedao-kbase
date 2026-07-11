#!/bin/bash
set -euo pipefail
: "${KBASE_REMOTE_URL:?KBASE_REMOTE_URL is required}"
: "${KBASE_SOURCE_AGENT_ID:?KBASE_SOURCE_AGENT_ID is required}"
: "${KBASE_SOURCE_AGENT_TOKEN:?KBASE_SOURCE_AGENT_TOKEN is required}"
if [[ -n "${KBASE_AUTH_TOKEN:-}" && "$KBASE_AUTH_TOKEN" == "$KBASE_SOURCE_AGENT_TOKEN" ]]; then
  echo "admin and source-agent tokens must differ" >&2; exit 1
fi
home="${HOME:?HOME is required}"
bin_dir="$home/Library/Application Support/KBase/bin"
state_dir="${SOURCE_AGENT_STATE_DIR:-$home/Library/Application Support/KBase/source-agent}"
plist="$home/Library/LaunchAgents/life.executor.kbase.source-agent.plist"
mkdir -p "$bin_dir" "$state_dir" "$(dirname "$plist")"
chmod 700 "$state_dir"
SOURCE_AGENT_OUTPUT="$bin_dir/source-agent" "$(dirname "$0")/build-source-agent-macos.sh" >/dev/null
printf '%s' "$KBASE_SOURCE_AGENT_TOKEN" | /usr/bin/security add-generic-password -U -s life.executor.kbase.source-agent -a "$KBASE_SOURCE_AGENT_ID:transport-token" -w
cat >"$plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>life.executor.kbase.source-agent</string>
<key>ProgramArguments</key><array><string>$bin_dir/source-agent</string><string>run</string></array>
<key>EnvironmentVariables</key><dict>
<key>KBASE_REMOTE_URL</key><string>$KBASE_REMOTE_URL</string>
<key>KBASE_SOURCE_AGENT_ID</key><string>$KBASE_SOURCE_AGENT_ID</string>
<key>SOURCE_AGENT_STATE_DIR</key><string>$state_dir</string>
<key>SOURCE_AGENT_ENROLL_ADDR</key><string>127.0.0.1:8765</string>
</dict><key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
</dict></plist>
PLIST
chmod 600 "$plist"
echo "installed; transport token remains in Keychain and MP enrollment is loopback-only"
