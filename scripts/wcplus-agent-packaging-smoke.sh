#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
build_script="$script_dir/build-wcplus-agent-macos.sh"
install_script="$script_dir/install-wcplus-agent-macos.sh"
uninstall_script="$script_dir/uninstall-wcplus-agent-macos.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/wcplus-agent-packaging.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

for script in "$build_script" "$install_script" "$uninstall_script" "$script_dir/lib/managed-worker-pair.sh" "$script_dir/lib/managed-worker-install.sh"; do
  bash -n "$script"
done

grep -Fq -- '--render-plist' "$install_script"
if ! grep -Fq 'mktemp "${TMPDIR:-/tmp}/wcplus-agent.plist.XXXXXX"' "$install_script"; then
  echo "WC Plus plist mktemp template is not suffix-unique" >&2
  exit 1
fi
first_plist_tmp="$(mktemp "$tmp_dir/wcplus-agent.plist.XXXXXX")"
second_plist_tmp="$(mktemp "$tmp_dir/wcplus-agent.plist.XXXXXX")"
if [[ "$first_plist_tmp" == "$second_plist_tmp" ]]; then
  echo "WC Plus consecutive plist temporary names collided" >&2
  exit 1
fi
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
  if compgen -G "$directory/*.tmp.*" >/dev/null || compgen -G "$directory/*.backup.*" >/dev/null ||
    [[ -e "$directory/.wcplus-agent.pair-journal" || -e "$directory/.wcplus-agent.pair-journal.tmp" ||
      -e "$directory/.wcplus-agent.pair-worker-old" || -e "$directory/.wcplus-agent.pair-updater-old" ]]; then
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
cat >"$tmp_dir/bin/wcplus-agent" <<'WORKER'
#!/bin/bash
set -euo pipefail
for name in KBASE_SOURCE_AGENT_TOKEN transport_token KBASE_AUTH_TOKEN admin_token; do
  [[ -z "${!name+x}" ]]
done
[[ "$*" == "check-config" ]]
case "${KBASE_REMOTE_URL:?}" in
  *\?* | *\#* | *://*@* | https:///*) exit 65 ;;
esac
case "${WCPLUSPRO_BASE_URL:-${WCPLUS_BASE_URL:-http://127.0.0.1:5001}}" in
  *\?* | *\#* | *://*@*) exit 66 ;;
esac
printf '%s\n' "$*" >"${WORKER_CAPTURE:?}"
if [[ -n "${WORKER_CALL_COUNT:-}" ]]; then
  worker_calls=0
  [[ ! -f "$WORKER_CALL_COUNT" ]] || read -r worker_calls <"$WORKER_CALL_COUNT"
  worker_calls=$((worker_calls + 1))
  printf '%s\n' "$worker_calls" >"$WORKER_CALL_COUNT"
  if [[ "${FAIL_INSTALLED_VALIDATION:-}" == true && $worker_calls -eq 2 ]]; then exit 94; fi
fi
WORKER
cat >"$tmp_dir/bin/source-agent-updater" <<'UPDATER'
#!/usr/bin/env bash
set -euo pipefail
for name in KBASE_SOURCE_AGENT_TOKEN transport_token KBASE_AUTH_TOKEN admin_token BASH_ENV ENV; do
  [[ -z "${!name+x}" ]]
done
[[ "$-" != *x* ]]
printf '%s\n' "$*" >"${UPDATER_CAPTURE:?}"
[[ "$*" == "--check --worker-type wcplus-worker" ]]
if [[ -n "${UPDATER_CALL_COUNT:-}" ]]; then
  updater_calls=0
  [[ ! -f "$UPDATER_CALL_COUNT" ]] || read -r updater_calls <"$UPDATER_CALL_COUNT"
  updater_calls=$((updater_calls + 1))
  printf '%s\n' "$updater_calls" >"$UPDATER_CALL_COUNT"
  if [[ "${FAIL_INSTALLED_UPDATER_CHECK:-}" == true && $updater_calls -eq 2 ]]; then
    exit 93
  fi
fi
UPDATER
cat >"$tmp_dir/hostile-startup.sh" <<'HOSTILE_STARTUP'
#!/bin/bash
: >"${HOSTILE_STARTUP_MARKER:?}"
HOSTILE_STARTUP
chmod 0755 "$tmp_dir/probe-bin/dirname" "$tmp_dir/probe-bin/grep" "$tmp_dir/bin/wcplus-agent" "$tmp_dir/bin/source-agent-updater"

function_shadow_marker="$tmp_dir/function-shadow-called"
set +e
printf '%s\n' 'function-shadow-token' | env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  'BASH_FUNC_dirname%%=() { printf "%s" "${transport_token-}" >"${FUNCTION_SHADOW_MARKER:?}"; /usr/bin/dirname "$@"; }' \
  'BASH_FUNC_sync%%=() { printf "%s" "${transport_token-}" >"${FUNCTION_SHADOW_MARKER:?}"; /bin/sync "$@"; }' \
  FUNCTION_SHADOW_MARKER="$function_shadow_marker" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  WORKER_CAPTURE="$tmp_dir/function-shadow-worker-args" \
  UPDATER_CAPTURE="$tmp_dir/function-shadow-updater-args" \
  "$install_script" --check >/dev/null 2>&1
function_shadow_status=$?
set -e
if [[ $function_shadow_status -ne 0 || -e "$function_shadow_marker" ]]; then
  echo "WC Plus installer executed an exported command-shadowing function" >&2
  exit 1
fi

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
  WORKER_CAPTURE="$tmp_dir/worker-args" \
  UPDATER_CAPTURE="$tmp_dir/updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/grep-called" \
  HOSTILE_STARTUP_MARKER="$tmp_dir/hostile-startup-called" \
  "$install_script" --render-plist >"$plist_fixture" 2>"$tmp_dir/render.stderr"

plutil -lint "$plist_fixture" >/dev/null
if [[ -e "$tmp_dir/first-child" || -e "$tmp_dir/hostile-startup-called" || -e "$tmp_dir/grep-called" ]]; then
  echo "WC Plus installer used hostile PATH/startup code or sent its token to grep" >&2
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
grep -Fxq -- 'check-config' "$tmp_dir/worker-args"

for invalid_remote in \
  'https://user@kbase.example.invalid' \
  'https://kbase.example.invalid/base?debug=1' \
  'https://kbase.example.invalid/base#fragment' \
  'https:///missing-host'; do
  set +e
  printf '%s\n' "$token_sentinel" | env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
    KBASE_REMOTE_URL="$invalid_remote" \
    KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
    WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
    WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
    PROBE_CAPTURE="$tmp_dir/invalid-first-child" \
    WORKER_CAPTURE="$tmp_dir/invalid-worker-args" \
    UPDATER_CAPTURE="$tmp_dir/invalid-updater-args" \
    GREP_CALLED_MARKER="$tmp_dir/invalid-grep-called" \
    "$install_script" --check >/dev/null 2>&1
  invalid_status=$?
  set -e
  if [[ $invalid_status -eq 0 ]]; then
    echo "WC Plus installer accepted invalid remote URL" >&2
    exit 1
  fi
done

set +e
printf '%s\n' "$token_sentinel" | env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  KBASE_REMOTE_URL="https://kbase.example.invalid/base" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  WCPLUSPRO_BASE_URL="http://127.0.0.1:5001/?debug=1" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  PROBE_CAPTURE="$tmp_dir/invalid-wcplus-first-child" \
  WORKER_CAPTURE="$tmp_dir/invalid-wcplus-worker-args" \
  UPDATER_CAPTURE="$tmp_dir/invalid-wcplus-updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/invalid-wcplus-grep-called" \
  "$install_script" --check >/dev/null 2>&1
invalid_wcplus_status=$?
set -e
if [[ $invalid_wcplus_status -eq 0 ]]; then
  echo "WC Plus installer accepted invalid local URL" >&2
  exit 1
fi

bounded_fifo="$tmp_dir/wcplus-bounded-token.fifo"
bounded_status_file="$tmp_dir/wcplus-bounded-token.status"
bounded_sent_file="$tmp_dir/wcplus-bounded-token.sent"
mkfifo "$bounded_fifo"
(
  set +e
  env -i PATH="$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
    KBASE_REMOTE_URL="https://kbase.example.invalid" \
    KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
    WCPLUS_AGENT_STATE_DIR="$tmp_dir/state" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
    PROBE_CAPTURE="$tmp_dir/bounded-first-child" \
    UPDATER_CAPTURE="$tmp_dir/bounded-updater-args" \
    GREP_CALLED_MARKER="$tmp_dir/bounded-grep-called" \
    "$install_script" --check <"$bounded_fifo" >/dev/null 2>&1
  printf '%s\n' "$?" >"$bounded_status_file"
) &
bounded_reader_pid=$!
(
  printf '%01025d' 0
  : >"$bounded_sent_file"
  sleep 5
) >"$bounded_fifo" &
bounded_writer_pid=$!
for ((attempt = 0; attempt < 100; attempt++)); do
  [[ -e "$bounded_status_file" ]] && break
  sleep 0.01
done
if [[ ! -e "$bounded_sent_file" || ! -e "$bounded_status_file" ]]; then
  kill "$bounded_reader_pid" "$bounded_writer_pid" 2>/dev/null || true
  wait "$bounded_reader_pid" "$bounded_writer_pid" 2>/dev/null || true
  echo "WC Plus installer did not cap transport-token input consumption" >&2
  exit 1
fi
bounded_status="$(<"$bounded_status_file")"
kill "$bounded_writer_pid" 2>/dev/null || true
wait "$bounded_reader_pid" "$bounded_writer_pid" 2>/dev/null || true
if [[ "$bounded_status" == 0 ]]; then
  echo "WC Plus installer accepted a bounded oversize transport token" >&2
  exit 1
fi

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
operation="${1:?}"
state_file="${LAUNCHCTL_STATE:?}"
failure_marker="${LAUNCHCTL_FAILURE_MARKER:-$state_file.no-failure}"
if [[ "$operation" == print ]]; then
  print_count=0
  [[ ! -f "${LAUNCHCTL_PRINT_COUNT:?}" ]] || read -r print_count <"$LAUNCHCTL_PRINT_COUNT"
  print_count=$((print_count + 1))
  printf '%s\n' "$print_count" >"$LAUNCHCTL_PRINT_COUNT"
  if [[ "${FAIL_LAUNCHCTL_OPERATION:-}" == final-print && $print_count -eq 2 && ! -e "$failure_marker" ]]; then
    : >"$failure_marker"
    exit 95
  fi
  if [[ -f "$state_file" && "$(<"$state_file")" == loaded ]]; then exit 0; else exit 113; fi
fi
: >"${LAUNCHCTL_MARKER:?}"
if [[ "${FAIL_LAUNCHCTL_OPERATION:-}" == "$operation" && ! -e "$failure_marker" ]]; then
  : >"$failure_marker"
  exit 95
fi
case "$operation" in
  bootout) printf 'unloaded\n' >"$state_file" ;;
  bootstrap)
    [[ ! -f "$state_file" || "$(<"$state_file")" != loaded ]] || exit 36
    printf 'loaded\n' >"$state_file"
    ;;
  kickstart) ;;
  *) exit 64 ;;
esac
FAKE_LAUNCHCTL
cat >"$tmp_dir/build-bin/security" <<'FAKE_SECURITY'
#!/bin/bash
set -euo pipefail
operation="${1:?}"
shift
account=""
while (($# > 0)); do
  case "$1" in
    -a) account="${2:?}"; shift 2 ;;
    *) shift ;;
  esac
done
case "$account" in
  transport-token) item="${KEYCHAIN_DIR:?}/main" ;;
  transport-token-install-backup) item="${KEYCHAIN_DIR:?}/backup" ;;
  *) exit 64 ;;
esac
case "$operation" in
  find-generic-password)
    [[ -f "$item" ]] || exit 44
    /bin/cat "$item"
    ;;
  add-generic-password)
    if [[ "${FAIL_SECURITY_MAIN_ADD:-false}" == true && "$account" == transport-token && ! -e "${SECURITY_FAILURE_MARKER:?}" ]]; then
      : >"$SECURITY_FAILURE_MARKER"
      exit 96
    fi
    IFS= read -r value
    IFS= read -r confirmation
    [[ "$value" == "$confirmation" ]]
    printf '%s\n' "$value" >"$item"
    ;;
  delete-generic-password)
    [[ -f "$item" ]] || exit 44
    /bin/rm -f "$item"
    ;;
  *) exit 64 ;;
esac
FAKE_SECURITY
chmod 0755 "$tmp_dir/build-bin/launchctl" "$tmp_dir/build-bin/security"
mkdir -p "$tmp_dir/home/keychain" "$tmp_dir/install-fixture/lib"
sed \
  -e "s|^PATH=/usr/bin:/bin:/usr/sbin:/sbin$|PATH=$tmp_dir/build-bin:/usr/bin:/bin:/usr/sbin:/sbin|" \
  -e 's|/usr/bin/security|security|g' \
  "$install_script" >"$tmp_dir/install-fixture/install-wcplus-agent-macos.sh"
cp "$script_dir/lib/managed-worker-pair.sh" "$tmp_dir/install-fixture/lib/managed-worker-pair.sh"
sed \
  -e 's|/usr/bin/security|security|g' \
  -e 's|/bin/launchctl|launchctl|g' \
  "$script_dir/lib/managed-worker-install.sh" >"$tmp_dir/install-fixture/lib/managed-worker-install.sh"
chmod 0755 "$tmp_dir/install-fixture/install-wcplus-agent-macos.sh"
transaction_install_script="$tmp_dir/install-fixture/install-wcplus-agent-macos.sh"
install_dir="$tmp_dir/home/install with spaces"
installed_plist="$tmp_dir/home/LaunchAgents/life.executor.kbase.wcplus-agent.plist"
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
  KEYCHAIN_DIR="$tmp_dir/home/keychain" \
  LAUNCHCTL_MARKER="$tmp_dir/launchctl-called" \
  PROBE_CAPTURE="$tmp_dir/install-first-child" \
  WORKER_CAPTURE="$tmp_dir/install-worker-args" \
  UPDATER_CAPTURE="$tmp_dir/install-updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/install-grep-called" \
  "$transaction_install_script" >/dev/null 2>&1
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
if compgen -G "$install_dir/*.tmp.*" >/dev/null || compgen -G "$install_dir/*.backup.*" >/dev/null ||
  [[ -e "$install_dir/.wcplus-agent.pair-journal" || -e "$install_dir/.wcplus-agent.pair-journal.tmp" ||
    -e "$install_dir/.wcplus-agent.pair-worker-old" || -e "$install_dir/.wcplus-agent.pair-updater-old" ]]; then
  echo "failed install publication left temporary or backup files" >&2
  exit 1
fi

printf 'old-worker' >"$installed_worker"
printf 'old-updater' >"$installed_updater"
printf 'old-plist' >"$installed_plist"
rm -f "$tmp_dir/updater-call-count" "$tmp_dir/launchctl-called"
set +e
printf '%s\n' "$token_sentinel" | env -i PATH="$tmp_dir/build-bin:$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
  KBASE_REMOTE_URL="https://kbase.example.invalid" \
  KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
  WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
  WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
  WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
  WCPLUS_AGENT_INSTALL_DIR="$install_dir" \
  WCPLUS_AGENT_STATE_DIR="$tmp_dir/install state" \
  WCPLUS_AGENT_LOG_DIR="$tmp_dir/install logs" \
  WCPLUS_AGENT_PLIST_PATH="$installed_plist" \
  FAIL_INSTALLED_UPDATER_CHECK=true \
  KEYCHAIN_DIR="$tmp_dir/home/keychain" \
  UPDATER_CALL_COUNT="$tmp_dir/updater-call-count" \
  LAUNCHCTL_MARKER="$tmp_dir/launchctl-called" \
  PROBE_CAPTURE="$tmp_dir/installed-updater-first-child" \
  WORKER_CAPTURE="$tmp_dir/installed-updater-worker-args" \
  UPDATER_CAPTURE="$tmp_dir/installed-updater-args" \
  GREP_CALLED_MARKER="$tmp_dir/installed-updater-grep-called" \
  "$transaction_install_script" >/dev/null 2>&1
installed_updater_status=$?
set -e
if [[ $installed_updater_status -eq 0 || "$(<"$installed_worker")" != "old-worker" || "$(<"$installed_updater")" != "old-updater" ]]; then
  echo "installed WC Plus updater failure did not restore the old artifact pair" >&2
  exit 1
fi
if [[ "$(<"$installed_plist")" != "old-plist" || -e "$tmp_dir/launchctl-called" ]]; then
  echo "installed WC Plus updater failure changed plist or launch state" >&2
  exit 1
fi
if ! grep -Fq 'managed_worker_install_begin' "$install_script"; then
  echo "WC Plus installer has no full install transaction before installed updater gate" >&2
  exit 1
fi

for failure_boundary in keychain validation plist bootout bootstrap kickstart final-print; do
  printf 'old-worker' >"$installed_worker"
  printf 'old-updater' >"$installed_updater"
  printf 'old-plist' >"$installed_plist"
  printf 'old-fixed-token\n' >"$tmp_dir/home/keychain/main"
  printf 'loaded\n' >"$tmp_dir/service-state"
  rm -f \
    "$tmp_dir/home/keychain/backup" \
    "$tmp_dir/security-failure" \
    "$tmp_dir/launchctl-failure" \
    "$tmp_dir/launchctl-called" \
    "$tmp_dir/launchctl-print-count" \
    "$tmp_dir/worker-call-count" \
    "$tmp_dir/updater-call-count" \
    "$tmp_dir/plist-publish-failure"
  fail_security=false
  fail_validation=false
  fail_plist_destination=""
  fail_launchctl=""
  case "$failure_boundary" in
    keychain) fail_security=true ;;
    validation) fail_validation=true ;;
    plist) fail_plist_destination="$installed_plist" ;;
    bootout | bootstrap | kickstart | final-print) fail_launchctl="$failure_boundary" ;;
  esac
  set +e
  printf '%s\n' 'new-fixed-token' | env -i PATH="$tmp_dir/build-bin:$tmp_dir/probe-bin:$PATH" HOME="$tmp_dir/home" \
    KBASE_REMOTE_URL="https://kbase.example.invalid" \
    KBASE_SOURCE_AGENT_ID="wcplus-agent-1" \
    WCPLUSPRO_BASE_URL="http://127.0.0.1:5001" \
    WCPLUS_AGENT_BINARY_PATH="$tmp_dir/bin/wcplus-agent" \
    WCPLUS_AGENT_UPDATER_BINARY_PATH="$tmp_dir/bin/source-agent-updater" \
    WCPLUS_AGENT_INSTALL_DIR="$install_dir" \
    WCPLUS_AGENT_STATE_DIR="$tmp_dir/install state" \
    WCPLUS_AGENT_LOG_DIR="$tmp_dir/install logs" \
    WCPLUS_AGENT_PLIST_PATH="$installed_plist" \
    KEYCHAIN_DIR="$tmp_dir/home/keychain" \
    FAIL_SECURITY_MAIN_ADD="$fail_security" \
    SECURITY_FAILURE_MARKER="$tmp_dir/security-failure" \
    FAIL_INSTALLED_VALIDATION="$fail_validation" \
    WORKER_CALL_COUNT="$tmp_dir/worker-call-count" \
    UPDATER_CALL_COUNT="$tmp_dir/updater-call-count" \
    FAIL_PUBLISH_DEST="$fail_plist_destination" \
    FAIL_PUBLISH_MARKER="$tmp_dir/plist-publish-failure" \
    FAIL_LAUNCHCTL_OPERATION="$fail_launchctl" \
    LAUNCHCTL_FAILURE_MARKER="$tmp_dir/launchctl-failure" \
    LAUNCHCTL_STATE="$tmp_dir/service-state" \
    LAUNCHCTL_PRINT_COUNT="$tmp_dir/launchctl-print-count" \
    LAUNCHCTL_MARKER="$tmp_dir/launchctl-called" \
    PROBE_CAPTURE="$tmp_dir/matrix-first-child" \
    WORKER_CAPTURE="$tmp_dir/matrix-worker-args" \
    UPDATER_CAPTURE="$tmp_dir/matrix-updater-args" \
    GREP_CALLED_MARKER="$tmp_dir/matrix-grep-called" \
    "$transaction_install_script" >"$tmp_dir/matrix-$failure_boundary.stdout" 2>"$tmp_dir/matrix-$failure_boundary.stderr"
  matrix_status=$?
  set -e
  if [[ $matrix_status -eq 0 ]]; then
    echo "WC Plus $failure_boundary fault unexpectedly succeeded" >&2
    exit 1
  fi
  if [[ "$(<"$installed_worker")" != old-worker || "$(<"$installed_updater")" != old-updater || "$(<"$installed_plist")" != old-plist ]]; then
    echo "WC Plus $failure_boundary fault did not restore old files" >&2
    exit 1
  fi
  if [[ "$(<"$tmp_dir/home/keychain/main")" != old-fixed-token || -e "$tmp_dir/home/keychain/backup" ]]; then
    echo "WC Plus $failure_boundary fault did not restore the old Keychain account" >&2
    exit 1
  fi
  if [[ "$(<"$tmp_dir/service-state")" != loaded ]]; then
    echo "WC Plus $failure_boundary fault did not restart the old service" >&2
    exit 1
  fi
  if compgen -G "$tmp_dir/home/Library/Application Support/KBase/.managed-worker-install.lock-ready.*" >/dev/null ||
    compgen -G "$tmp_dir/home/Library/Application Support/KBase/.managed-worker-install.lock-release.*" >/dev/null ||
    [[ -e "$tmp_dir/home/Library/Application Support/KBase/.managed-worker-install-journal" ||
    -e "$tmp_dir/home/Library/Application Support/KBase/.managed-worker-install-journal.tmp" ||
    -e "$tmp_dir/home/Library/Application Support/KBase/.managed-worker-install-plist-old" ||
    -e "$install_dir/.wcplus-agent.pair-journal" ||
    -e "$install_dir/.wcplus-agent.pair-journal.tmp" ||
    -e "$install_dir/.wcplus-agent.pair-worker-old" ||
    -e "$install_dir/.wcplus-agent.pair-updater-old" ]]; then
    echo "WC Plus $failure_boundary fault left transaction state" >&2
    exit 1
  fi
  if grep -Fq 'new-fixed-token' "$tmp_dir/matrix-$failure_boundary.stdout" "$tmp_dir/matrix-$failure_boundary.stderr" ||
    grep -Fq 'old-fixed-token' "$tmp_dir/matrix-$failure_boundary.stdout" "$tmp_dir/matrix-$failure_boundary.stderr"; then
    echo "WC Plus $failure_boundary fault leaked Keychain material" >&2
    exit 1
  fi
done
publish_line="$(grep -n 'managed_worker_pair_publish .*installed_worker.*installed_updater' "$install_script" | cut -d: -f1)"
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
grep -Fq 'lib/managed-worker-pair.sh' "$build_script"
grep -Fq 'lib/managed-worker-pair.sh' "$install_script"
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
grep -Fxq '#!/usr/bin/env -S -u BASH_ENV -u ENV -u SHELLOPTS /bin/bash -p' "$install_script"

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
