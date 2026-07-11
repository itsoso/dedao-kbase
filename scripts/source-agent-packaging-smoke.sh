#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
for file in build-source-agent-macos.sh install-source-agent-macos.sh uninstall-source-agent-macos.sh; do
  test -x "$root/scripts/$file"
done
grep -q 'GOARCH=arm64' "$root/scripts/build-source-agent-macos.sh"
grep -q 'CODESIGN_IDENTITY' "$root/scripts/build-source-agent-macos.sh"
grep -q 'chmod 600' "$root/scripts/install-source-agent-macos.sh"
grep -q 'chmod 700' "$root/scripts/install-source-agent-macos.sh"
grep -q '127.0.0.1' "$root/scripts/install-source-agent-macos.sh"
grep -q 'KBASE_SOURCE_AGENT_TOKEN' "$root/scripts/install-source-agent-macos.sh"
if grep -Eq 'WECHAT_MP_(TOKEN|COOKIE)' "$root/scripts/install-source-agent-macos.sh"; then
  echo "MP secrets must not be written to the plist" >&2; exit 1
fi
grep -q -- '--purge-state' "$root/scripts/uninstall-source-agent-macos.sh"
echo "source-agent packaging smoke passed"
