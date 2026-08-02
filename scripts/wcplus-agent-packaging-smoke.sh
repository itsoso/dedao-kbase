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

mkdir -p "$tmp_dir/build-bin"
cat >"$tmp_dir/build-bin/go" <<'FAKE_GO'
#!/bin/bash
set -euo pipefail
all_args="$*"
output=""
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-o" ]]; then
    shift
    output="${1:-}"
  fi
  shift || true
done
[[ -n "$output" ]]
if [[ -n "${GO_CALLED_MARKER:-}" ]]; then
  : >"$GO_CALLED_MARKER"
fi
case "$all_args" in
  *./cmd/source-agent-updater*) printf 'new-updater' >"$output" ;;
  *./cmd/wcplus-agent*) printf 'new-worker' >"$output" ;;
  *) exit 64 ;;
esac
FAKE_GO
cat >"$tmp_dir/build-bin/mv" <<'FAKE_MV'
#!/bin/bash
set -euo pipefail
destination=""
for destination in "$@"; do :; done
if [[ -n "${FAIL_PUBLISH_DEST:-}" && "$destination" == "$FAIL_PUBLISH_DEST" && ! -e "${FAIL_PUBLISH_MARKER:?}" ]]; then
  : >"$FAIL_PUBLISH_MARKER"
  exit 55
fi
exec /bin/mv "$@"
FAKE_MV
chmod 0755 "$tmp_dir/build-bin/go" "$tmp_dir/build-bin/mv"

run_fixture_build() {
  env PATH="$tmp_dir/build-bin:$PATH" \
    WCPLUS_AGENT_BINARY_PATH="$1" \
    WCPLUS_AGENT_UPDATER_BINARY_PATH="$2" \
    FAIL_PUBLISH_DEST="${FAIL_PUBLISH_DEST:-}" \
    FAIL_PUBLISH_MARKER="${FAIL_PUBLISH_MARKER:-$tmp_dir/no-failure}" \
    GO_CALLED_MARKER="${GO_CALLED_MARKER:-}" \
    bash "$build_script" >/dev/null 2>&1
}

assert_no_publish_debris() {
  local directory="$1"
  if compgen -G "$directory/*.tmp.*" >/dev/null || compgen -G "$directory/*.backup.*" >/dev/null; then
    echo "build publication left temporary or backup files" >&2
    exit 1
  fi
}

publish_dir="$tmp_dir/publish with spaces"
mkdir -p "$publish_dir"
worker_output="$publish_dir/wcplus-agent"
updater_output="$publish_dir/source-agent-updater"
printf 'old-worker' >"$worker_output"
printf 'old-updater' >"$updater_output"
failure_marker="$tmp_dir/build-second-publish-failed"
set +e
FAIL_PUBLISH_DEST="$updater_output" FAIL_PUBLISH_MARKER="$failure_marker" \
  run_fixture_build "$worker_output" "$updater_output"
publish_status=$?
set -e
if [[ $publish_status -eq 0 || "$(<"$worker_output")" != "old-worker" || "$(<"$updater_output")" != "old-updater" ]]; then
  echo "failed build publication did not restore the old artifact pair" >&2
  exit 1
fi
assert_no_publish_debris "$publish_dir"

for missing in worker updater; do
  printf 'old-worker' >"$worker_output"
  printf 'old-updater' >"$updater_output"
  rm -f "$publish_dir/build-second-publish-failed"
  if [[ "$missing" == "worker" ]]; then rm -f "$worker_output"; else rm -f "$updater_output"; fi
  set +e
  FAIL_PUBLISH_DEST="$updater_output" FAIL_PUBLISH_MARKER="$publish_dir/build-second-publish-failed" \
    run_fixture_build "$worker_output" "$updater_output"
  publish_status=$?
  set -e
  if [[ $publish_status -eq 0 ]]; then
    echo "missing-old build fixture unexpectedly succeeded" >&2
    exit 1
  fi
  if [[ "$missing" == "worker" ]]; then
    [[ ! -e "$worker_output" && "$(<"$updater_output")" == "old-updater" ]]
  else
    [[ "$(<"$worker_output")" == "old-worker" && ! -e "$updater_output" ]]
  fi
  assert_no_publish_debris "$publish_dir"
done

different_worker="$tmp_dir/worker-parent/wcplus-agent"
different_updater="$tmp_dir/updater-parent/source-agent-updater"
mkdir -p "$(dirname "$different_worker")" "$(dirname "$different_updater")"
printf 'old-worker' >"$different_worker"
printf 'old-updater' >"$different_updater"
rm -f "$tmp_dir/go-called"
set +e
GO_CALLED_MARKER="$tmp_dir/go-called" run_fixture_build "$different_worker" "$different_updater"
different_status=$?
set -e
if [[ $different_status -eq 0 || -e "$tmp_dir/go-called" || "$(<"$different_worker")" != "old-worker" || "$(<"$different_updater")" != "old-updater" ]]; then
  echo "build did not reject different artifact parent directories before mutation" >&2
  exit 1
fi

run_fixture_build "$worker_output" "$updater_output"
if [[ "$(<"$worker_output")" != "new-worker" || "$(<"$updater_output")" != "new-updater" ]]; then
  echo "successful build did not publish the new artifact pair" >&2
  exit 1
fi
assert_no_publish_debris "$publish_dir"

mkdir -p "$tmp_dir/home" "$tmp_dir/bin" "$tmp_dir/probe-bin"
cat >"$tmp_dir/probe-bin/dirname" <<'PROBE'
#!/bin/bash
set -euo pipefail
for name in KBASE_SOURCE_AGENT_TOKEN transport_token KBASE_AUTH_TOKEN admin_token BASH_ENV ENV; do
  if [[ -n "${!name+x}" ]]; then
    printf 'leaked:%s\n' "$name" >"${PROBE_CAPTURE:?}"
    exit 91
  fi
done
[[ "$-" != *x* ]]
printf 'clean\n' >"${PROBE_CAPTURE:?}"
exec /usr/bin/dirname "$@"
PROBE
cat >"$tmp_dir/probe-bin/grep" <<'PROBE_GREP'
#!/bin/bash
set -euo pipefail
: >"${GREP_CALLED_MARKER:?}"
exit 92
PROBE_GREP
printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp_dir/bin/wcplus-agent"
cat >"$tmp_dir/bin/source-agent-updater" <<'UPDATER'
#!/usr/bin/env bash
set -euo pipefail
for name in KBASE_SOURCE_AGENT_TOKEN transport_token KBASE_AUTH_TOKEN admin_token BASH_ENV ENV; do
  [[ -z "${!name+x}" ]]
done
[[ "$-" != *x* ]]
printf '%s\n' "$*" >"${UPDATER_CAPTURE:?}"
[[ "$*" == "--check --worker-type wcplus-worker" ]]
UPDATER
cat >"$tmp_dir/hostile-startup.sh" <<'HOSTILE_STARTUP'
#!/bin/bash
: >"${HOSTILE_STARTUP_MARKER:?}"
HOSTILE_STARTUP
chmod 0755 "$tmp_dir/probe-bin/dirname" "$tmp_dir/probe-bin/grep" "$tmp_dir/bin/wcplus-agent" "$tmp_dir/bin/source-agent-updater"

set +e
missing_output="$({
  env -i PATH="$PATH" HOME="$tmp_dir/home" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
    "$install_script" --check </dev/null
} 2>&1)"
missing_status=$?
set -e
if [[ $missing_status -eq 0 ]]; then
  echo "install self-check unexpectedly accepted missing configuration" >&2
  exit 1
fi
for name in KBASE_REMOTE_URL KBASE_SOURCE_AGENT_ID WCPLUS_AGENT_STATE_DIR; do
  if ! grep -Fq "$name" <<<"$missing_output"; then
    echo "install self-check did not report $name" >&2
    exit 1
  fi
done
if grep -Fq 'KBASE_SOURCE_AGENT_TOKEN' <<<"$missing_output"; then
  echo "install self-check still requires KBASE_SOURCE_AGENT_TOKEN from the environment" >&2
  exit 1
fi

token_sentinel='agent<&>secret-sentinel'
set +e
env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  KBASE_SOURCE_AGENT_TOKEN="$token_sentinel" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  PROBE_CAPTURE="$tmp_dir/env-token-first-child" \
  UPDATER_CAPTURE="$tmp_dir/env-token-updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/env-token-grep-called" \
  "$install_script" --check </dev/null >"$tmp_dir/env-token.stdout" 2>"$tmp_dir/env-token.stderr"
env_token_status=$?
set -e
if [[ $env_token_status -eq 0 ]]; then
  echo "WC Plus installer accepted KBASE_SOURCE_AGENT_TOKEN from the environment" >&2
  exit 1
fi
if grep -Fq "$token_sentinel" "$tmp_dir/env-token.stdout" "$tmp_dir/env-token.stderr"; then
  echo "WC Plus installer leaked the rejected environment token" >&2
  exit 1
fi

plist_fixture="$tmp_dir/wcplus-agent.plist"
printf '%s\n' "$token_sentinel" | env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  transport_token="preexisting-export" \
  admin_token="preexisting-admin-export" \
  KBASE_AUTH_TOKEN="different-admin-token" \
  BASH_ENV="$tmp_dir/hostile-startup.sh" \
  ENV="$tmp_dir/hostile-startup.sh" \
  SHELLOPTS="xtrace" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
  WCPLUS_AGENT_LOG_DIR="$tmp_dir/logs" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  PROBE_CAPTURE="$tmp_dir/first-child" \
  UPDATER_CAPTURE="$tmp_dir/updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/grep-called" \
  HOSTILE_STARTUP_MARKER="$tmp_dir/hostile-startup-called" \
  "$install_script" --render-plist >"$plist_fixture" 2>"$tmp_dir/render.stderr"

plutil -lint "$plist_fixture" >/dev/null
grep -Fxq 'clean' "$tmp_dir/first-child"
if [[ -e "$tmp_dir/hostile-startup-called" || -e "$tmp_dir/grep-called" ]]; then
  echo "WC Plus installer executed hostile startup code or sent its token to grep" >&2
  exit 1
fi
if grep -Fq "$token_sentinel" "$tmp_dir/render.stderr"; then
  echo "WC Plus installer leaked its stdin token through xtrace" >&2
  exit 1
fi
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

oversize_token=""
for ((index = 0; index < 1025; index++)); do oversize_token+="x"; done
set +e
printf '%s\n' "$oversize_token" | env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  PROBE_CAPTURE="$tmp_dir/oversize-first-child" \
  UPDATER_CAPTURE="$tmp_dir/oversize-updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/oversize-grep-called" \
  "$install_script" --check >/dev/null 2>&1
oversize_status=$?
set -e
if [[ $oversize_status -eq 0 ]]; then
  echo "WC Plus installer accepted an oversize transport token" >&2
  exit 1
fi
unset oversize_token

cat >"$tmp_dir/build-bin/launchctl" <<'FAKE_LAUNCHCTL'
#!/bin/bash
set -euo pipefail
: >"${LAUNCHCTL_MARKER:?}"
exit 0
FAKE_LAUNCHCTL
chmod 0755 "$tmp_dir/build-bin/launchctl"
install_dir="$tmp_dir/install with spaces"
installed_plist="$tmp_dir/LaunchAgents/life.executor.kbase.wcplus-agent.plist"
mkdir -p "$install_dir" "$(dirname "$installed_plist")"
canonical_install_dir="$(cd "$install_dir" && pwd -P)"
installed_worker="$canonical_install_dir/wcplus-agent"
installed_updater="$canonical_install_dir/source-agent-updater"
printf 'old-worker' >"$installed_worker"
printf 'old-updater' >"$installed_updater"
printf 'old-plist' >"$installed_plist"
rm -f "$tmp_dir/install-second-publish-failed" "$tmp_dir/launchctl-called"
set +e
printf '%s\n' "$token_sentinel" | env -i PATH="$tmp_dir/build-bin:$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  transport_token="preexisting-export" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  WCPLUS_AGENT_INSTALL_DIR="$install_dir" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/install state" \
  WCPLUS_AGENT_LOG_DIR="$tmp_dir/install logs" \
  WCPLUS_AGENT_PLIST_PATH="$installed_plist" \
  FAIL_PUBLISH_DEST="$installed_updater" \
  FAIL_PUBLISH_MARKER="$tmp_dir/install-second-publish-failed" \
  LAUNCHCTL_MARKER="$tmp_dir/launchctl-called" \
  PROBE_CAPTURE="$tmp_dir/install-first-child" \
  UPDATER_CAPTURE="$tmp_dir/install-updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/install-grep-called" \
  "$install_script" >/dev/null 2>&1
install_status=$?
set -e
if [[ $install_status -eq 0 || "$(<"$installed_worker")" != "old-worker" || "$(<"$installed_updater")" != "old-updater" ]]; then
  echo "failed install publication did not restore the old artifact pair" >&2
  exit 1
fi
if [[ "$(<"$installed_plist")" != "old-plist" || -e "$tmp_dir/launchctl-called" ]]; then
  echo "failed install publication changed plist or launch state" >&2
  exit 1
fi
if compgen -G "$install_dir/*.tmp.*" >/dev/null || compgen -G "$install_dir/*.backup.*" >/dev/null || compgen -G "$install_dir/.*-agent.*" >/dev/null; then
  echo "failed install publication left temporary or backup files" >&2
  exit 1
fi
publish_line="$(grep -n 'publish_artifact_pair .*installed_worker.*installed_updater' "$install_script" | cut -d: -f1)"
security_line="$(grep -n '/usr/bin/security add-generic-password' "$install_script" | cut -d: -f1)"
if [[ -z "$publish_line" || -z "$security_line" ]] || ((publish_line >= security_line)); then
  echo "artifact pair must publish before Keychain mutation" >&2
  exit 1
fi

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
grep -Fq 'unset KBASE_AUTH_TOKEN KBASE_SOURCE_AGENT_TOKEN' "$install_script"
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
grep -Fxq '#!/usr/bin/env -S -u BASH_ENV -u ENV -u SHELLOPTS /bin/bash' "$install_script"

if [[ "$repo_root" == "$tmp_dir" ]]; then
  echo "invalid repository root" >&2
  exit 1
fi
for script in "$build_script" "$install_script"; do
  if grep -Eq 'CODESIGN_IDENTITY|(^|[^[:alnum:]_])codesign([^[:alnum:]_]|$)' "$script"; then
    echo "managed worker packaging must not expose a signing configuration" >&2
    exit 1
  fi
done

echo "wcplus agent packaging smoke passed"
