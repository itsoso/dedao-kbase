#!/usr/bin/env -S -u BASH_ENV -u ENV -u SHELLOPTS /bin/bash -p
set +x
set -euo pipefail
set +a
umask 077
IFS=$' \t\n'
unset CDPATH
LC_ALL=C
export LC_ALL
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH

if [[ -n "${KBASE_SOURCE_AGENT_TOKEN+x}" ]]; then
  echo "KBASE_SOURCE_AGENT_TOKEN environment input is not supported; provide the token on standard input" >&2
  exit 2
fi
unset transport_token admin_token source_agent_token
admin_token="${KBASE_AUTH_TOKEN-}"
unset KBASE_AUTH_TOKEN KBASE_SOURCE_AGENT_TOKEN BASH_ENV ENV

mode="install"

usage() {
  echo "usage: install-wcplus-agent-macos.sh [--check|--render-plist]" >&2
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

required_names=(KBASE_REMOTE_URL KBASE_SOURCE_AGENT_ID WCPLUS_AGENT_STATE_DIR)
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
transport_token=""
if ! IFS= read -r transport_token; then
  echo "source-agent transport token is required on standard input" >&2
  exit 2
fi
if [[ -n "$admin_token" && "$admin_token" == "$transport_token" ]]; then
  echo "admin and source-agent tokens must differ" >&2
  exit 2
fi
unset admin_token

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
pair_library="$script_dir/lib/managed-worker-pair.sh"
install_library="$script_dir/lib/managed-worker-install.sh"
# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$pair_library"
# shellcheck source=scripts/lib/managed-worker-install.sh
source "$install_library"
home="${HOME:?HOME is required}"
label="life.executor.kbase.wcplus-agent"
worker_type="wcplus-worker"
transport_token_service="life.executor.kbase.source-agent"
transport_token_account="transport-token"
max_transport_token_bytes=1024
binary_source="${WCPLUS_AGENT_BINARY_PATH:-$repo_root/build/bin/wcplus-agent}"
updater_source="${WCPLUS_AGENT_UPDATER_BINARY_PATH:-$repo_root/build/bin/source-agent-updater}"
install_dir="${WCPLUS_AGENT_INSTALL_DIR:-$home/Library/Application Support/dedao-kbase/bin}"
plist_path="${WCPLUS_AGENT_PLIST_PATH:-$home/Library/LaunchAgents/$label.plist}"
state_dir="${WCPLUS_AGENT_STATE_DIR:-}"
log_dir="${WCPLUS_AGENT_LOG_DIR:-$home/Library/Logs/dedao-kbase/wcplus-agent}"
poll_seconds="${WCPLUS_AGENT_POLL_SECONDS:-15}"
restart_seconds="${WCPLUS_AGENT_RESTART_SECONDS:-30}"
wcplus_url="${WCPLUSPRO_BASE_URL:-${WCPLUS_BASE_URL:-http://127.0.0.1:5001}}"

for command_name in cat cp install launchctl mktemp mv plutil rm rmdir sed shasum sync wc; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing required command: $command_name" >&2
    exit 1
  fi
done
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "wcplus-agent installation requires macOS" >&2
  exit 1
fi
if [[ ! -x "$binary_source" ]]; then
  echo "WCPLUS_AGENT_BINARY_PATH must point to an executable" >&2
  exit 2
fi
if [[ ! -x "$updater_source" ]]; then
  echo "WCPLUS_AGENT_UPDATER_BINARY_PATH must point to an executable" >&2
  exit 2
fi
token_valid=true
if ((${#transport_token} == 0 || ${#transport_token} > max_transport_token_bytes)); then
  token_valid=false
else
  for ((index = 0; index < ${#transport_token}; index++)); do
    case "${transport_token:index:1}" in
      [[:graph:]]) ;;
      *) token_valid=false; break ;;
    esac
  done
fi
if [[ "$token_valid" != true ]]; then
  echo "KBASE_SOURCE_AGENT_TOKEN must contain printable ASCII without spaces" >&2
  exit 2
fi
unset token_valid index
if [[ ! "$poll_seconds" =~ ^[0-9]+$ ]] || ((poll_seconds < 1 || poll_seconds > 300)); then
  echo "WCPLUS_AGENT_POLL_SECONDS must be between 1 and 300" >&2
  exit 2
fi
if [[ ! "$restart_seconds" =~ ^[0-9]+$ ]] || ((restart_seconds < 10 || restart_seconds > 300)); then
  echo "WCPLUS_AGENT_RESTART_SECONDS must be between 10 and 300" >&2
  exit 2
fi

if ! KBASE_REMOTE_URL="$KBASE_REMOTE_URL" \
  KBASE_SOURCE_AGENT_ID="$KBASE_SOURCE_AGENT_ID" \
  WCPLUSPRO_BASE_URL="$wcplus_url" \
  WCPLUS_AGENT_STATE_DIR="$state_dir" \
  "$binary_source" check-config >/dev/null 2>&1; then
  echo "WC Plus configuration preflight failed" >&2
  exit 2
fi

if ! "$updater_source" --check --worker-type "$worker_type" >/dev/null 2>&1; then
  echo "WC Plus updater preflight failed" >&2
  exit 1
fi

xml_escape() {
  printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

render_plist() {
  local destination="$1"
  local installed_binary="$2"
  local label_xml binary_xml remote_xml agent_id_xml wcplus_xml state_xml stdout_xml stderr_xml
  label_xml="$(xml_escape "$label")"
  binary_xml="$(xml_escape "$installed_binary")"
  remote_xml="$(xml_escape "$KBASE_REMOTE_URL")"
  agent_id_xml="$(xml_escape "$KBASE_SOURCE_AGENT_ID")"
  wcplus_xml="$(xml_escape "$wcplus_url")"
  state_xml="$(xml_escape "$state_dir")"
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
    <key>WCPLUSPRO_BASE_URL</key>
    <string>$wcplus_xml</string>
    <key>WCPLUS_AGENT_STATE_DIR</key>
    <string>$state_xml</string>
    <key>WCPLUS_AGENT_POLL_SECONDS</key>
    <string>$poll_seconds</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>$restart_seconds</integer>
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

tmp_plist="$(mktemp "${TMPDIR:-/tmp}/wcplus-agent.plist.XXXXXX")"
worker_tmp=""
updater_tmp=""
plist_tmp=""
cleanup() {
  local status=$?
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" == true ]]; then
    if ! managed_worker_install_rollback; then status=1; fi
  elif [[ "$MANAGED_WORKER_PAIR_ACTIVE" == true ]] && ! managed_worker_pair_rollback; then
    status=1
  fi
  rm -f "$tmp_plist" || status=1
  [[ -z "$worker_tmp" ]] || rm -f "$worker_tmp" || status=1
  [[ -z "$updater_tmp" ]] || rm -f "$updater_tmp" || status=1
  [[ -z "$plist_tmp" ]] || rm -f "$plist_tmp" || status=1
  return "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ "$mode" == "render" ]]; then
  render_plist "$tmp_plist" "$binary_source"
  cat "$tmp_plist"
  exit 0
fi
if [[ "$mode" == "check" ]]; then
  render_plist "$tmp_plist" "$binary_source"
  echo "wcplus-agent installation configuration is valid"
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
installed_worker="$install_dir/wcplus-agent"
installed_updater="$install_dir/source-agent-updater"
domain="gui/$(id -u)"

worker_tmp="$install_dir/.wcplus-agent.$$"
updater_tmp="$install_dir/.source-agent-updater.$$"
install -m 0755 "$binary_source" "$worker_tmp"
install -m 0755 "$updater_source" "$updater_tmp"
if ! managed_worker_install_begin "$home" wcplus-agent "$installed_worker" "$installed_updater" "$plist_path" "$domain" "$label"; then
  echo "WC Plus installation transaction initialization failed" >&2
  exit 1
fi
if ! managed_worker_pair_publish "$worker_tmp" "$updater_tmp" "$installed_worker" "$installed_updater"; then
  echo "WC Plus artifact installation failed" >&2
  exit 1
fi
worker_tmp=""
updater_tmp=""
if ! managed_worker_install_mark published; then
  echo "WC Plus installation transaction update failed" >&2
  exit 1
fi
if ! "$install_dir/source-agent-updater" --check --worker-type wcplus-worker >/dev/null 2>&1; then
  echo "installed WC Plus updater preflight failed" >&2
  exit 1
fi

if ! managed_worker_install_mark keychain; then
  echo "WC Plus installation transaction update failed" >&2
  exit 1
fi
if ! printf '%s\n%s\n' "$transport_token" "$transport_token" | \
  /usr/bin/security add-generic-password -U -s "$transport_token_service" -a "$transport_token_account" -w >/dev/null 2>&1; then
  echo "store source-agent transport token failed" >&2
  exit 1
fi
unset transport_token

if ! KBASE_REMOTE_URL="$KBASE_REMOTE_URL" \
  KBASE_SOURCE_AGENT_ID="$KBASE_SOURCE_AGENT_ID" \
  WCPLUSPRO_BASE_URL="$wcplus_url" \
  WCPLUS_AGENT_STATE_DIR="$state_dir" \
  "$installed_worker" check-config >/dev/null 2>&1; then
  echo "installed WC Plus configuration validation failed" >&2
  exit 1
fi

render_plist "$tmp_plist" "$installed_worker"
plist_tmp="$plist_path.tmp.$$"
install -m 0600 "$tmp_plist" "$plist_tmp"
if ! managed_worker_install_mark plist; then
  echo "WC Plus installation transaction update failed" >&2
  exit 1
fi
mv -f "$plist_tmp" "$plist_path"
plist_tmp=""

if ! managed_worker_install_mark launching; then
  echo "WC Plus installation transaction update failed" >&2
  exit 1
fi
if [[ "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" == 1 ]]; then
  launchctl bootout "$domain/$label"
fi
launchctl bootstrap "$domain" "$plist_path"
launchctl kickstart -k "$domain/$label"
launchctl print "$domain/$label" >/dev/null
if ! managed_worker_install_commit; then
  echo "WC Plus installation commit failed" >&2
  exit 1
fi
echo "wcplus-agent installed and started"
