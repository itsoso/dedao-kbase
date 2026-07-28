#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
template="${1:-${root}/deploy/nginx/kbase.locations.conf.template}"
output="${2:-}"
browser_secret="${KBASE_BROWSER_SESSION_SECRET:-}"
backend_addr="${KBASE_BACKEND_ADDR:-127.0.0.1:8719}"
basic_auth_file="${KBASE_BASIC_AUTH_FILE:-/etc/dedao-kbase/browser-basic-auth.htpasswd}"

if [[ -z "${output}" ]]; then
  echo "usage: KBASE_BROWSER_SESSION_SECRET=... $0 [template] <output>" >&2
  exit 2
fi
if [[ ! "${browser_secret}" =~ ^[A-Za-z0-9_-]{32,128}$ ]]; then
  echo "KBASE_BROWSER_SESSION_SECRET must contain 32-128 URL-safe ASCII characters" >&2
  exit 2
fi
if [[ ! "${backend_addr}" =~ ^(127\.0\.0\.1|localhost|\[::1\]):[0-9]{1,5}$ ]]; then
  echo "KBASE_BACKEND_ADDR must be a loopback host and port" >&2
  exit 2
fi
backend_port="${backend_addr##*:}"
if ((10#${backend_port} < 1 || 10#${backend_port} > 65535)); then
  echo "KBASE_BACKEND_ADDR port must be between 1 and 65535" >&2
  exit 2
fi
if [[ ! "${basic_auth_file}" =~ ^/[A-Za-z0-9_./-]+$ ]]; then
  echo "KBASE_BASIC_AUTH_FILE must be a safe absolute path" >&2
  exit 2
fi
if [[ ! -f "${template}" ]]; then
  echo "Nginx template not found" >&2
  exit 2
fi

location_block() {
  local marker="$1"
  awk -v marker="${marker}" '
    $0 == marker {
      active = 1
    }
    active {
      print
    }
    active && $0 == "}" {
      exit
    }
  ' "${template}"
}

require_exact_location() {
  local marker="$1"
  local count
  count="$(awk -v marker="${marker}" '$0 == marker { count++ } END { print count + 0 }' "${template}")"
  if [[ "${count}" != "1" ]]; then
    echo "Nginx template must contain exactly one ${marker}" >&2
    exit 2
  fi
}

require_block_line() {
  local block="$1"
  local line="$2"
  local label="$3"
  if ! grep -Fq "${line}" <<<"${block}"; then
    echo "${label} is missing required directive: ${line}" >&2
    exit 2
  fi
}

reject_block_line() {
  local block="$1"
  local line="$2"
  local label="$3"
  if grep -Fq "${line}" <<<"${block}"; then
    echo "${label} contains forbidden directive: ${line}" >&2
    exit 2
  fi
}

for marker in \
  'location = /browser/session {' \
  'location = /browser/session/migrate {' \
  'location = /browser/session-token {' \
  'location /api/ {' \
  'location / {'; do
  require_exact_location "${marker}"
done

login_block="$(location_block 'location = /browser/session {')"
require_block_line "${login_block}" 'auth_basic "dedao-kbase";' "browser session login location"
require_block_line "${login_block}" 'auth_basic_user_file __KBASE_BASIC_AUTH_FILE__;' "browser session login location"
require_block_line "${login_block}" 'proxy_set_header Authorization "";' "browser session login location"
require_block_line "${login_block}" 'proxy_set_header Proxy-Authorization "";' "browser session login location"
require_block_line "${login_block}" 'proxy_set_header X-KBase-Browser-Session "__KBASE_BROWSER_SESSION_SECRET__";' "browser session login location"

migration_block="$(location_block 'location = /browser/session/migrate {')"
require_block_line "${migration_block}" 'auth_basic off;' "browser session migration location"
require_block_line "${migration_block}" 'proxy_set_header Proxy-Authorization "";' "browser session migration location"
require_block_line "${migration_block}" 'proxy_set_header X-KBase-Browser-Session "";' "browser session migration location"
reject_block_line "${migration_block}" 'auth_basic_user_file' "browser session migration location"
reject_block_line "${migration_block}" 'proxy_set_header Authorization "";' "browser session migration location"
reject_block_line "${migration_block}" '__KBASE_BROWSER_SESSION_SECRET__' "browser session migration location"

retired_block="$(location_block 'location = /browser/session-token {')"
require_block_line "${retired_block}" 'auth_basic off;' "retired browser token location"
require_block_line "${retired_block}" 'proxy_set_header Authorization "";' "retired browser token location"
require_block_line "${retired_block}" 'proxy_set_header Proxy-Authorization "";' "retired browser token location"
require_block_line "${retired_block}" 'proxy_set_header X-KBase-Browser-Session "";' "retired browser token location"
reject_block_line "${retired_block}" 'auth_basic_user_file' "retired browser token location"
reject_block_line "${retired_block}" '__KBASE_BROWSER_SESSION_SECRET__' "retired browser token location"

for marker in 'location /api/ {' 'location / {'; do
  block="$(location_block "${marker}")"
  require_block_line "${block}" 'proxy_set_header Proxy-Authorization "";' "${marker}"
  require_block_line "${block}" 'proxy_set_header X-KBase-Browser-Session "";' "${marker}"
  reject_block_line "${block}" 'proxy_set_header Authorization "";' "${marker}"
done

browser_secret_placeholder_count="$(
  awk 'index($0, "__KBASE_BROWSER_SESSION_SECRET__") { count++ } END { print count + 0 }' "${template}"
)"
if [[ "${browser_secret_placeholder_count}" != "1" ]]; then
  echo "Nginx template must inject the browser session secret exactly once" >&2
  exit 2
fi

for placeholder in \
  __KBASE_BROWSER_SESSION_SECRET__ \
  __KBASE_BACKEND_ADDR__ \
  __KBASE_BASIC_AUTH_FILE__; do
  if ! grep -q "${placeholder}" "${template}"; then
    echo "Nginx template is missing required placeholders" >&2
    exit 2
  fi
done

output_dir="$(dirname "${output}")"
mkdir -p "${output_dir}"
umask 077
temporary="$(mktemp "${output}.tmp.XXXXXX")"
cleanup() {
  rm -f "${temporary}"
}
trap cleanup EXIT

while IFS= read -r line || [[ -n "${line}" ]]; do
  line="${line//__KBASE_BROWSER_SESSION_SECRET__/${browser_secret}}"
  line="${line//__KBASE_BACKEND_ADDR__/${backend_addr}}"
  line="${line//__KBASE_BASIC_AUTH_FILE__/${basic_auth_file}}"
  printf '%s\n' "${line}"
done <"${template}" >"${temporary}"
if grep -Eq '__[A-Z0-9_]+__' "${temporary}"; then
  echo "Nginx configuration contains an unresolved placeholder" >&2
  exit 2
fi
chmod 600 "${temporary}"
mv "${temporary}" "${output}"
trap - EXIT
