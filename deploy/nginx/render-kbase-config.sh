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

active_directive_count() {
  local block="$1"
  local expected="$2"
  awk -v expected="${expected}" '
    function normalize(value) {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      gsub(/[[:space:]]+/, " ", value)
      return value
    }
    BEGIN {
      expected = normalize(expected)
    }
    {
      directive = normalize($0)
      if (directive == "" || substr(directive, 1, 1) == "#") {
        next
      }
      if (directive == expected) {
        count++
      }
    }
    END {
      print count + 0
    }
  ' <<<"${block}"
}

active_directive_prefix_count() {
  local block="$1"
  local prefix="$2"
  awk -v prefix="${prefix}" '
    function normalize(value) {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      gsub(/[[:space:]]+/, " ", value)
      return value
    }
    BEGIN {
      prefix = normalize(prefix)
    }
    {
      directive = normalize($0)
      if (directive == "" || substr(directive, 1, 1) == "#") {
        next
      }
      if (directive == prefix || index(directive, prefix " ") == 1) {
        count++
      }
    }
    END {
      print count + 0
    }
  ' <<<"${block}"
}

require_only_active_directive() {
  local block="$1"
  local prefix="$2"
  local expected="$3"
  local label="$4"
  local prefix_count
  local exact_count
  prefix_count="$(active_directive_prefix_count "${block}" "${prefix}")"
  exact_count="$(active_directive_count "${block}" "${expected}")"
  if [[ "${prefix_count}" != "1" || "${exact_count}" != "1" ]]; then
    echo "${label} must contain exactly one active directive: ${expected}" >&2
    exit 2
  fi
}

require_no_active_directive() {
  local block="$1"
  local prefix="$2"
  local label="$3"
  local count
  count="$(active_directive_prefix_count "${block}" "${prefix}")"
  if [[ "${count}" != "0" ]]; then
    echo "${label} contains forbidden active directive prefix: ${prefix}" >&2
    exit 2
  fi
}

for marker in \
  'location = /health {' \
  'location = /.well-known/dedao-kbase-skills.json {' \
  'location = /browser/session {' \
  'location = /browser/session/migrate {' \
  'location = /browser/session-token {' \
  'location /api/ {' \
  'location / {'; do
  require_exact_location "${marker}"
done

for marker in \
  'location = /health {' \
  'location = /.well-known/dedao-kbase-skills.json {' \
  'location = /browser/session {' \
  'location = /browser/session/migrate {' \
  'location = /browser/session-token {' \
  'location /api/ {' \
  'location / {'; do
  block="$(location_block "${marker}")"
  require_only_active_directive \
    "${block}" \
    'proxy_pass ' \
    'proxy_pass http://__KBASE_BACKEND_ADDR__;' \
    "${marker}"
  require_only_active_directive \
    "${block}" \
    'proxy_set_header Proxy-Authorization ' \
    'proxy_set_header Proxy-Authorization "";' \
    "${marker}"
done

login_block="$(location_block 'location = /browser/session {')"
require_only_active_directive \
  "${login_block}" \
  'auth_basic ' \
  'auth_basic "dedao-kbase";' \
  "browser session login location"
require_only_active_directive \
  "${login_block}" \
  'auth_basic_user_file ' \
  'auth_basic_user_file __KBASE_BASIC_AUTH_FILE__;' \
  "browser session login location"
require_only_active_directive \
  "${login_block}" \
  'proxy_set_header X-KBase-Browser-Session ' \
  'proxy_set_header X-KBase-Browser-Session "__KBASE_BROWSER_SESSION_SECRET__";' \
  "browser session login location"

migration_block="$(location_block 'location = /browser/session/migrate {')"
require_only_active_directive \
  "${migration_block}" \
  'auth_basic ' \
  'auth_basic off;' \
  "browser session migration location"
require_no_active_directive \
  "${migration_block}" \
  'auth_basic_user_file ' \
  "browser session migration location"

retired_block="$(location_block 'location = /browser/session-token {')"
require_only_active_directive \
  "${retired_block}" \
  'auth_basic ' \
  'auth_basic off;' \
  "retired browser token location"
require_no_active_directive \
  "${retired_block}" \
  'auth_basic_user_file ' \
  "retired browser token location"

for marker in \
  'location = /health {' \
  'location = /.well-known/dedao-kbase-skills.json {' \
  'location = /browser/session {' \
  'location = /browser/session-token {' \
  'location / {'; do
  block="$(location_block "${marker}")"
  require_only_active_directive \
    "${block}" \
    'proxy_set_header Authorization ' \
    'proxy_set_header Authorization "";' \
    "${marker}"
done

for marker in \
  'location = /browser/session/migrate {' \
  'location /api/ {'; do
  block="$(location_block "${marker}")"
  require_no_active_directive \
    "${block}" \
    'proxy_set_header Authorization ' \
    "${marker}"
done

for marker in \
  'location = /health {' \
  'location = /.well-known/dedao-kbase-skills.json {' \
  'location = /browser/session/migrate {' \
  'location = /browser/session-token {' \
  'location /api/ {' \
  'location / {'; do
  block="$(location_block "${marker}")"
  require_only_active_directive \
    "${block}" \
    'proxy_set_header X-KBase-Browser-Session ' \
    'proxy_set_header X-KBase-Browser-Session "";' \
    "${marker}"
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
