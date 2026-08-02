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

grep -Fq -- '--render-plist' "$install_script"
bash "$build_script" --check >/dev/null

mkdir -p "$tmp_dir/home" "$tmp_dir/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp_dir/bin/wcplus-agent"
cat >"$tmp_dir/bin/source-agent-updater" <<'UPDATER'
#!/usr/bin/env bash
set -euo pipefail
[[ -z "${KBASE_SOURCE_AGENT_TOKEN+x}" ]]
printf '%s\n' "$*" >"${UPDATER_CAPTURE:?}"
[[ "$*" == "--check --worker-type wcplus-worker" ]]
UPDATER
chmod 0755 "$tmp_dir/bin/wcplus-agent" "$tmp_dir/bin/source-agent-updater"

set +e
missing_output="$({
  env -i PATH="$PATH" HOME="$tmp_dir/home" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
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

token_sentinel='agent<&>secret-sentinel'
plist_fixture="$tmp_dir/wcplus-agent.plist"
env -i PATH="$PATH" HOME="$tmp_dir/home" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  KBASE_SOURCE_AGENT_TOKEN="$token_sentinel" \
  WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
  WCPLUS_AGENT_LOG_DIR="$tmp_dir/logs" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  UPDATER_CAPTURE="$tmp_dir/updater-args" \
  bash "$install_script" --render-plist >"$plist_fixture"

plutil -lint "$plist_fixture" >/dev/null
if grep -Fq 'KBASE_SOURCE_AGENT_TOKEN' "$plist_fixture" || grep -Fq "$token_sentinel" "$plist_fixture"; then
  echo "WC Plus LaunchAgent plist contains the transport token" >&2
  exit 1
fi
grep -Fq 'life.executor.kbase.wcplus-agent' "$plist_fixture"
if grep -Fq 'life.executor.kbase.source-agent</string>' "$plist_fixture"; then
  echo "WC Plus plist uses the WeChat worker identity" >&2
  exit 1
fi
grep -Fq "$tmp_dir/bin/wcplus-agent" "$plist_fixture"
grep -Fq "$tmp_dir/state" "$plist_fixture"
grep -Fq "$tmp_dir/logs" "$plist_fixture"
grep -Fxq -- '--check --worker-type wcplus-worker' "$tmp_dir/updater-args"

env -i PATH="$PATH" HOME="$tmp_dir/home" \
  WCPLUS_AGENT_INSTALL_DIR="$tmp_dir/install" \
  WCPLUS_AGENT_PLIST_PATH="$tmp_dir/LaunchAgents/life.executor.kbase.wcplus-agent.plist" \
  bash "$uninstall_script" --check >/dev/null

grep -Fq './cmd/wcplus-agent' "$build_script"
grep -Fq './cmd/source-agent-updater' "$build_script"
grep -Fq 'GOOS=darwin' "$build_script"
if grep -Eq 'codesign .*updater' "$build_script"; then
  echo "Task 9 must not introduce updater signing" >&2
  exit 1
fi
grep -Fq 'source-agent-updater' "$install_script"
grep -Fq 'transport-token' "$install_script"
grep -Fq 'life.executor.kbase.source-agent' "$install_script"
grep -Fq 'unset KBASE_SOURCE_AGENT_TOKEN' "$install_script"
grep -Fq ' -w' "$install_script"
grep -Fq '/usr/bin/security add-generic-password -U -s "$transport_token_service" -a "$transport_token_account" -w' "$install_script"
if grep -Fq -- '-w "$KBASE_SOURCE_AGENT_TOKEN"' "$install_script" || grep -Fq -- '-w "$transport_token"' "$install_script"; then
  echo "security command must not receive the transport token in argv" >&2
  exit 1
fi
grep -Fq 'install -m 0755' "$install_script"
grep -Fq 'install -m 0600' "$install_script"
grep -Fq 'chmod 0700' "$install_script"
grep -Fq 'source-agent-updater" --check --worker-type wcplus-worker' "$install_script"
grep -Fq -- '--delete-state' "$uninstall_script"
grep -Fq 'State preserved' "$uninstall_script"

if [[ "$repo_root" == "$tmp_dir" ]]; then
  echo "invalid repository root" >&2
  exit 1
fi

echo "wcplus agent packaging smoke passed"
