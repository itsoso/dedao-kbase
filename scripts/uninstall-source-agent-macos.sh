#!/bin/bash
set -euo pipefail
purge=false
if [[ "${1:-}" == "--purge-state" ]]; then purge=true; elif [[ $# -gt 0 ]]; then echo "usage: $0 [--purge-state]" >&2; exit 2; fi
home="${HOME:?HOME is required}"
plist="$home/Library/LaunchAgents/life.executor.kbase.source-agent.plist"
launchctl bootout "gui/$(id -u)/life.executor.kbase.source-agent" 2>/dev/null || true
rm -f "$plist" "$home/Library/Application Support/KBase/bin/source-agent"
if $purge; then rm -rf "$home/Library/Application Support/KBase/source-agent"; fi
echo "uninstalled; state and imported data preserved unless --purge-state was provided"
