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
