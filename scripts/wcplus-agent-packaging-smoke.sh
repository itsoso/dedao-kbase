#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
build_script="$script_dir/build-wcplus-agent-macos.sh"
install_script="$script_dir/install-wcplus-agent-macos.sh"
uninstall_script="$script_dir/uninstall-wcplus-agent-macos.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/wcplus-agent-packaging.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

for script in "$build_script" "$install_script" "$uninstall_script"; do
  bash -n "$script"
done

bash "$build_script" --check >/dev/null

mkdir -p "$tmp_dir/home" "$tmp_dir/bin"
printf '#!/usr/bin/env bash\nexit 0\n' > "$tmp_dir/bin/wcplus-agent"
chmod 0755 "$tmp_dir/bin/wcplus-agent"

set +e
missing_output="$({
  env -i PATH="$PATH" HOME="$tmp_dir/home" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    bash "$install_script" --check
} 2>&1)"
missing_status=$?
set -e
if [[ $missing_status -eq 0 ]]; then
  echo "install self-check unexpectedly accepted missing configuration" >&2
  exit 1
fi
for name in KBASE_REMOTE_URL KBASE_SOURCE_AGENT_ID KBASE_SOURCE_AGENT_TOKEN WCPLUS_AGENT_STATE_DIR; do
  if ! grep -Fq "$name" <<<"$missing_output"; then
    echo "install self-check did not report $name" >&2
    exit 1
  fi
done

token_sentinel="agent<&>secret-sentinel"
check_output="$({
  env -i PATH="$PATH" HOME="$tmp_dir/home" \
    KBASE_REMOTE_URL="https://kbase.example.invalid" \
    KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
    KBASE_SOURCE_AGENT_TOKEN="$token_sentinel" \
    WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
    WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
    WCPLUS_AGENT_LOG_DIR="$tmp_dir/logs" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    WCPLUS_AGENT_INSTALL_DIR="$tmp_dir/install" \
    WCPLUS_AGENT_PLIST_PATH="$tmp_dir/LaunchAgents/life.executor.kbase.wcplus-agent.plist" \
    bash "$install_script" --check
} 2>&1)"
if grep -Fq "$token_sentinel" <<<"$check_output"; then
  echo "install self-check exposed the source-agent token" >&2
  exit 1
fi

env -i PATH="$PATH" HOME="$tmp_dir/home" \
  WCPLUS_AGENT_INSTALL_DIR="$tmp_dir/install" \
  WCPLUS_AGENT_PLIST_PATH="$tmp_dir/LaunchAgents/life.executor.kbase.wcplus-agent.plist" \
  bash "$uninstall_script" --check >/dev/null

grep -Fq '<key>SuccessfulExit</key>' "$install_script"
grep -Fq '<false/>' "$install_script"
grep -Fq '<key>ThrottleInterval</key>' "$install_script"
grep -Fq 'install -m 0600' "$install_script"
grep -Fq -- '--delete-state' "$uninstall_script"
grep -Fq 'State preserved' "$uninstall_script"
grep -Fq 'go build' "$build_script"
grep -Fq './cmd/wcplus-agent' "$build_script"

if [[ "$repo_root" == "$tmp_dir" ]]; then
  echo "invalid repository root" >&2
  exit 1
fi

echo "wcplus agent packaging smoke passed"
