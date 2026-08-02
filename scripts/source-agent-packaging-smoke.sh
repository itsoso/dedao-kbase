#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
build_script="$script_dir/build-source-agent-macos.sh"
install_script="$script_dir/install-source-agent-macos.sh"
uninstall_script="$script_dir/uninstall-source-agent-macos.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/source-agent-packaging.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

for script in "$build_script" "$install_script" "$uninstall_script"; do
  bash -n "$script"
done
grep -Fxq '#!/bin/bash' "$install_script"

grep -Fq -- '--check' "$build_script"
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
  *./cmd/source-agent*) printf 'new-worker' >"$output" ;;
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
    SOURCE_AGENT_BINARY_PATH="$1" \
    SOURCE_AGENT_UPDATER_BINARY_PATH="$2" \
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
worker_output="$publish_dir/source-agent"
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

different_worker="$tmp_dir/worker-parent/source-agent"
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
if [[ -n "${KBASE_SOURCE_AGENT_TOKEN+x}" || -n "${transport_token+x}" ]]; then
  printf 'leaked\n' >"${PROBE_CAPTURE:?}"
  exit 91
fi
printf 'clean\n' >"${PROBE_CAPTURE:?}"
exec /usr/bin/dirname "$@"
PROBE
printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp_dir/bin/source-agent"
cat >"$tmp_dir/bin/source-agent-updater" <<'UPDATER'
#!/usr/bin/env bash
set -euo pipefail
[[ -z "${KBASE_SOURCE_AGENT_TOKEN+x}" ]]
[[ -z "${transport_token+x}" ]]
printf '%s\n' "$*" >"${UPDATER_CAPTURE:?}"
[[ "$*" == "--check --worker-type wechat-worker" ]]
UPDATER
chmod 0755 "$tmp_dir/probe-bin/dirname" "$tmp_dir/bin/source-agent" "$tmp_dir/bin/source-agent-updater"

token_sentinel='agent<&>secret-sentinel'
plist_fixture="$tmp_dir/source-agent.plist"
env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  transport_token="preexisting-export" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="source-agent-1" \
  KBASE_SOURCE_AGENT_TOKEN="$token_sentinel" \
  SOURCE_AGENT_BINARY_PATH="$tmp_dir/bin/source-agent" \
  SOURCE_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  SOURCE_AGENT_STATE_DIR="$tmp_dir/state" \
  SOURCE_AGENT_LOG_DIR="$tmp_dir/logs" \
  PROBE_CAPTURE="$tmp_dir/first-child" \
  UPDATER_CAPTURE="$tmp_dir/updater-args" \
  bash "$install_script" --render-plist >"$plist_fixture"

plutil -lint "$plist_fixture" >/dev/null
grep -Fxq 'clean' "$tmp_dir/first-child"
if grep -Fq 'KBASE_SOURCE_AGENT_TOKEN' "$plist_fixture" || grep -Fq "$token_sentinel" "$plist_fixture"; then
  echo "source-agent LaunchAgent plist contains the transport token" >&2
  exit 1
fi
grep -Fq 'life.executor.kbase.source-agent' "$plist_fixture"
if grep -Fq 'life.executor.kbase.wcplus-agent' "$plist_fixture"; then
  echo "source-agent plist uses the WC Plus worker identity" >&2
  exit 1
fi
grep -Fq "$tmp_dir/bin/source-agent" "$plist_fixture"
grep -Fq "$tmp_dir/state" "$plist_fixture"
grep -Fq "$tmp_dir/logs" "$plist_fixture"
grep -Fxq -- '--check --worker-type wechat-worker' "$tmp_dir/updater-args"

oversize_token=""
for ((index = 0; index < 1025; index++)); do oversize_token+="x"; done
set +e
env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="source-agent-1" \
  KBASE_SOURCE_AGENT_TOKEN="$oversize_token" \
  SOURCE_AGENT_BINARY_PATH="$tmp_dir/bin/source-agent" \
  SOURCE_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  SOURCE_AGENT_STATE_DIR="$tmp_dir/state" \
  PROBE_CAPTURE="$tmp_dir/oversize-first-child" \
  UPDATER_CAPTURE="$tmp_dir/oversize-updater-args" \
  bash "$install_script" --check >/dev/null 2>&1
oversize_status=$?
set -e
if [[ $oversize_status -eq 0 ]]; then
  echo "source-agent installer accepted an oversize transport token" >&2
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
installed_plist="$tmp_dir/LaunchAgents/life.executor.kbase.source-agent.plist"
mkdir -p "$install_dir" "$(dirname "$installed_plist")"
canonical_install_dir="$(cd "$install_dir" && pwd -P)"
installed_worker="$canonical_install_dir/source-agent"
installed_updater="$canonical_install_dir/source-agent-updater"
printf 'old-worker' >"$installed_worker"
printf 'old-updater' >"$installed_updater"
printf 'old-plist' >"$installed_plist"
rm -f "$tmp_dir/install-second-publish-failed" "$tmp_dir/launchctl-called"
set +e
env -i PATH="$tmp_dir/build-bin:$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  transport_token="preexisting-export" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="source-agent-1" \
  KBASE_SOURCE_AGENT_TOKEN="$token_sentinel" \
  SOURCE_AGENT_BINARY_PATH="$tmp_dir/bin/source-agent" \
  SOURCE_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  SOURCE_AGENT_INSTALL_DIR="$install_dir" \
  SOURCE_AGENT_STATE_DIR="$tmp_dir/install state" \
  SOURCE_AGENT_LOG_DIR="$tmp_dir/install logs" \
  SOURCE_AGENT_PLIST_PATH="$installed_plist" \
  FAIL_PUBLISH_DEST="$installed_updater" \
  FAIL_PUBLISH_MARKER="$tmp_dir/install-second-publish-failed" \
  LAUNCHCTL_MARKER="$tmp_dir/launchctl-called" \
  PROBE_CAPTURE="$tmp_dir/install-first-child" \
  UPDATER_CAPTURE="$tmp_dir/install-updater-args" \
  bash "$install_script" >/dev/null 2>&1
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

grep -Fq './cmd/source-agent' "$build_script"
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
grep -Fq 'source-agent-updater" --check --worker-type wechat-worker' "$install_script"
grep -Fq 'keychainEnvelopePrefix' "$repo_root/cmd/source-agent/keychain_store_darwin.go"
grep -Fq 'sealKeychainEnvelope' "$repo_root/cmd/source-agent/keychain_store_darwin.go"
grep -Fq -- '--purge-state' "$uninstall_script"
for script in "$build_script" "$install_script"; do
  if grep -Eq 'CODESIGN_IDENTITY|(^|[^[:alnum:]_])codesign([^[:alnum:]_]|$)' "$script"; then
    echo "managed worker packaging must not expose a signing configuration" >&2
    exit 1
  fi
done

echo "source-agent packaging smoke passed"
