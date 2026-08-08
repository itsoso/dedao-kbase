#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
server_bin="${KBASE_SERVER_BIN:-}"
nginx_bin="${NGINX_BIN:-nginx}"
backend_port="${KBASE_PROXY_SMOKE_BACKEND_PORT:-18719}"
proxy_port="${KBASE_PROXY_SMOKE_NGINX_PORT:-18880}"

if [[ -z "${server_bin}" || ! -x "${server_bin}" ]]; then
  echo "KBASE_SERVER_BIN must point to an executable kbase-server" >&2
  exit 2
fi
for command in "${nginx_bin}" curl openssl; do
  if ! command -v "${command}" >/dev/null 2>&1 && [[ ! -x "${command}" ]]; then
    echo "missing required command" >&2
    exit 2
  fi
done

temporary="$(mktemp -d)"
nginx_error_args=()
if "${nginx_bin}" -h 2>&1 | grep -Fq ' -e '; then
  nginx_error_args=(-e "${temporary}/nginx-error.log")
fi
backend_pid=""
nginx_started=0
cleanup() {
  if [[ "${nginx_started}" -eq 1 ]]; then
    "${nginx_bin}" \
      -p "${temporary}/nginx-prefix" \
      -c "${temporary}/nginx.conf" \
      "${nginx_error_args[@]}" \
      -s quit >/dev/null 2>&1 || true
  fi
  if [[ -n "${backend_pid}" ]]; then
    kill "${backend_pid}" >/dev/null 2>&1 || true
    wait "${backend_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${temporary}"
}
trap cleanup EXIT

mkdir -p \
  "${temporary}/books" \
  "${temporary}/config" \
  "${temporary}/nginx-prefix" \
  "${temporary}/nginx-prefix/client-body" \
  "${temporary}/nginx-prefix/proxy" \
  "${temporary}/nginx-prefix/fastcgi" \
  "${temporary}/nginx-prefix/uwsgi" \
  "${temporary}/nginx-prefix/scgi" \
  "${temporary}/web"
printf '%s\n' '<!doctype html><title>KBase smoke</title>' >"${temporary}/web/index.html"

api_value="$(openssl rand -hex 32)"
browser_proxy_value="$(openssl rand -hex 32)"
session_admin_value="$(openssl rand -hex 32)"
basic_user="kbase-smoke"
basic_password="$(openssl rand -hex 16)"
basic_hash="$(openssl passwd -apr1 "${basic_password}")"
printf '%s:%s\n' "${basic_user}" "${basic_hash}" >"${temporary}/browser.htpasswd"
# Nginx workers need to traverse the random directory and read only this
# ephemeral password file. Directory listing and the rendered proxy secret
# remain inaccessible.
chmod 711 "${temporary}"
chmod 644 "${temporary}/browser.htpasswd"

KBASE_HTTP_ADDR="127.0.0.1:${backend_port}" \
KBASE_AUTH_TOKEN="${api_value}" \
KBASE_BROWSER_SESSION_SECRET="${browser_proxy_value}" \
KBASE_BROWSER_SESSION_DB_PATH="${temporary}/state/browser_sessions.sqlite3" \
KBASE_PUBLIC_ORIGIN="http://127.0.0.1:${proxy_port}" \
KBASE_SESSION_ADMIN_TOKEN="${session_admin_value}" \
KBASE_BOOK_KNOWLEDGE_ROOT="${temporary}/books" \
KBASE_WEB_DIR="${temporary}/web" \
DEDAO_GO_CONFIG_DIR="${temporary}/config" \
"${server_bin}" >"${temporary}/backend.log" 2>&1 &
backend_pid=$!

backend_health="http://127.0.0.1:${backend_port}/health"
for _ in $(seq 1 50); do
  if curl -fsS "${backend_health}" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
curl -fsS "${backend_health}" >/dev/null

KBASE_BROWSER_SESSION_SECRET="${browser_proxy_value}" \
KBASE_BACKEND_ADDR="127.0.0.1:${backend_port}" \
KBASE_BASIC_AUTH_FILE="${temporary}/browser.htpasswd" \
bash "${root}/deploy/nginx/render-kbase-config.sh" \
  "${root}/deploy/nginx/kbase.locations.conf.template" \
  "${temporary}/locations.conf"

sed \
  's/^[[:space:]]*auth_basic "dedao-kbase";/    # auth_basic "dedao-kbase";/' \
  "${root}/deploy/nginx/kbase.locations.conf.template" \
  >"${temporary}/commented-required-directive.template"
if KBASE_BROWSER_SESSION_SECRET="${browser_proxy_value}" \
  KBASE_BACKEND_ADDR="127.0.0.1:${backend_port}" \
  KBASE_BASIC_AUTH_FILE="${temporary}/browser.htpasswd" \
  bash "${root}/deploy/nginx/render-kbase-config.sh" \
    "${temporary}/commented-required-directive.template" \
    "${temporary}/commented-required-directive.conf" >/dev/null 2>&1; then
  echo "renderer accepted a commented-out required security directive" >&2
  exit 1
fi

sed \
  '1,/^[[:space:]]*proxy_pass http:\/\/__KBASE_BACKEND_ADDR__;/{
    s/^[[:space:]]*proxy_pass http:\/\/__KBASE_BACKEND_ADDR__;/    # proxy_pass http:\/\/__KBASE_BACKEND_ADDR__;/
  }' \
  "${root}/deploy/nginx/kbase.locations.conf.template" \
  >"${temporary}/commented-proxy-pass.template"
if KBASE_BROWSER_SESSION_SECRET="${browser_proxy_value}" \
  KBASE_BACKEND_ADDR="127.0.0.1:${backend_port}" \
  KBASE_BASIC_AUTH_FILE="${temporary}/browser.htpasswd" \
  bash "${root}/deploy/nginx/render-kbase-config.sh" \
    "${temporary}/commented-proxy-pass.template" \
    "${temporary}/commented-proxy-pass.conf" >/dev/null 2>&1; then
  echo "renderer accepted a commented-out required proxy_pass" >&2
  exit 1
fi

for required in \
  'location = /browser/session {' \
  'location = /browser/session/migrate {' \
  'location = /browser/session-token {' \
  'proxy_connect_timeout 10s;' \
  'proxy_send_timeout 120s;' \
  'proxy_read_timeout 120s;' \
  'proxy_buffering off;'; do
  if ! grep -Fq "${required}" "${temporary}/locations.conf"; then
    echo "rendered Nginx configuration is missing: ${required}" >&2
    exit 1
  fi
done

cat >"${temporary}/nginx.conf" <<EOF
worker_processes 1;
pid ${temporary}/nginx.pid;
error_log ${temporary}/nginx-error.log;
events { worker_connections 64; }
http {
    access_log off;
    client_body_temp_path ${temporary}/nginx-prefix/client-body;
    proxy_temp_path ${temporary}/nginx-prefix/proxy;
    fastcgi_temp_path ${temporary}/nginx-prefix/fastcgi;
    uwsgi_temp_path ${temporary}/nginx-prefix/uwsgi;
    scgi_temp_path ${temporary}/nginx-prefix/scgi;
    server {
        listen 127.0.0.1:${proxy_port};
        server_name localhost;
        include ${temporary}/locations.conf;
    }
}
EOF
"${nginx_bin}" \
  -p "${temporary}/nginx-prefix" \
  -c "${temporary}/nginx.conf" \
  "${nginx_error_args[@]}" \
  -t >/dev/null
"${nginx_bin}" \
  -p "${temporary}/nginx-prefix" \
  -c "${temporary}/nginx.conf" \
  "${nginx_error_args[@]}"
nginx_started=1

proxy_base="http://127.0.0.1:${proxy_port}"
backend_base="http://127.0.0.1:${backend_port}"
public_origin="${proxy_base}"
client_id="smoke-browser-client-$(openssl rand -hex 12)"
second_client_id="smoke-migration-client-$(openssl rand -hex 12)"

response_status() {
  awk '/^HTTP\// { status = $2 } END { print status }' "$1"
}

response_cookie() {
  sed -nE 's/^[Ss]et-[Cc]ookie: (__Host-kbase_session=[^;]+).*/\1/p' "$1" | head -n 1
}

json_string() {
  local key="$1"
  local file="$2"
  sed -nE "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"([^\"]*)\".*/\\1/p" "${file}" | head -n 1
}

json_number() {
  local key="$1"
  local file="$2"
  sed -nE "s/.*\"${key}\"[[:space:]]*:[[:space:]]*([0-9]+).*/\\1/p" "${file}" | head -n 1
}

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
  ' "${temporary}/locations.conf"
}

block_has_active_directive() {
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
    /^[[:space:]]*#/ {
      next
    }
    normalize($0) == expected {
      found = 1
    }
    END {
      exit found ? 0 : 1
    }
  ' <<<"${block}"
}

for marker in \
  'location = /health {' \
  'location = /.well-known/dedao-kbase-skills.json {' \
  'location = /browser/session {' \
  'location = /browser/session-token {' \
  'location / {'; do
  if ! block_has_active_directive \
    "$(location_block "${marker}")" \
    'proxy_set_header Authorization "";'; then
    echo "${marker} does not strip client Authorization" >&2
    exit 1
  fi
done

health_block="$(location_block 'location = /health {')"
if ! block_has_active_directive \
  "$health_block" \
  'add_header Cache-Control "no-store" always;'; then
  echo "health location does not disable downstream caching" >&2
  exit 1
fi
if ! block_has_active_directive "$health_block" 'proxy_no_cache 1;'; then
  echo "health location does not disable proxy caching" >&2
  exit 1
fi
for marker in \
  'location = /browser/session/migrate {' \
  'location /api/ {'; do
  if block_has_active_directive \
    "$(location_block "${marker}")" \
    'proxy_set_header Authorization "";'; then
    echo "${marker} unexpectedly strips client Authorization" >&2
    exit 1
  fi
done

curl -fsS "${proxy_base}/" >/dev/null

curl -sS -D "${temporary}/login-no-basic.headers" \
  -o "${temporary}/login-no-basic.body" \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  "${proxy_base}/browser/session"
if [[ "$(response_status "${temporary}/login-no-basic.headers")" != "401" ]] ||
  ! grep -Eiq '^WWW-Authenticate:[[:space:]]*Basic' "${temporary}/login-no-basic.headers"; then
  echo "browser session without Basic Auth was not challenged" >&2
  exit 1
fi

wrong_basic_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -u "${basic_user}:wrong" \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  "${proxy_base}/browser/session")"
if [[ "${wrong_basic_status}" != "401" ]]; then
  echo "browser session with invalid Basic Auth returned HTTP ${wrong_basic_status}, want 401" >&2
  exit 1
fi

curl -sS -D "${temporary}/epoch.headers" \
  -o "${temporary}/epoch.body" \
  -u "${basic_user}:${basic_password}" \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  -H "X-KBase-Browser-Session: attacker-controlled" \
  "${proxy_base}/browser/session"
if [[ "$(response_status "${temporary}/epoch.headers")" != "200" ]]; then
  echo "browser client epoch acquisition failed" >&2
  exit 1
fi
epoch="$(json_number epoch "${temporary}/epoch.body")"
if [[ -z "${epoch}" ]]; then
  echo "browser client epoch was missing" >&2
  exit 1
fi

curl -sS -D "${temporary}/login.headers" \
  -o "${temporary}/login.body" \
  -X POST \
  -u "${basic_user}:${basic_password}" \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  -H "X-KBase-Browser-Epoch: ${epoch}" \
  -H "X-KBase-Browser-Session: attacker-controlled" \
  "${proxy_base}/browser/session"
if [[ "$(response_status "${temporary}/login.headers")" != "200" ]]; then
  echo "browser session login failed" >&2
  exit 1
fi
if ! grep -Eiq '^Set-Cookie:[[:space:]]*__Host-kbase_session=[^;]+;[[:space:]]*Path=/;.*HttpOnly;[[:space:]]*Secure;[[:space:]]*SameSite=Strict' \
  "${temporary}/login.headers" ||
  grep -Eiq '^Set-Cookie:.*;[[:space:]]*Domain=' "${temporary}/login.headers"; then
  echo "browser session login did not return the required secure Cookie" >&2
  exit 1
fi
if grep -Fq "${api_value}" "${temporary}/login.body" ||
  grep -Eq '"(api_)?token"[[:space:]]*:' "${temporary}/login.body"; then
  echo "browser session login leaked an API token" >&2
  exit 1
fi
session_cookie="$(response_cookie "${temporary}/login.headers")"
if [[ -z "${session_cookie}" ]]; then
  echo "browser session Cookie was missing" >&2
  exit 1
fi

if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Cookie: ${session_cookie}" \
  "${proxy_base}/api/books")" != "200" ]]; then
  echo "Cookie-authenticated API request failed" >&2
  exit 1
fi

curl -sS -D "${temporary}/status.headers" \
  -o "${temporary}/status.body" \
  -H "Cookie: ${session_cookie}" \
  "${proxy_base}/api/browser/session"
if [[ "$(response_status "${temporary}/status.headers")" != "200" ]]; then
  echo "browser session status request failed" >&2
  exit 1
fi
csrf_token="$(json_string csrf_token "${temporary}/status.body")"
if [[ -z "${csrf_token}" ]]; then
  echo "browser session status did not issue CSRF" >&2
  exit 1
fi

missing_csrf_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST \
  -H "Cookie: ${session_cookie}" \
  -H "Origin: ${public_origin}" \
  -H "Sec-Fetch-Site: same-origin" \
  "${proxy_base}/api/browser/session/logout")"
if [[ "${missing_csrf_status}" != "403" ]] ||
  [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
    -H "Cookie: ${session_cookie}" \
    "${proxy_base}/api/books")" != "200" ]]; then
  echo "missing-CSRF logout was not rejected without revoking the session" >&2
  exit 1
fi

invalid_csrf_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST \
  -H "Cookie: ${session_cookie}" \
  -H "Origin: ${public_origin}" \
  -H "Sec-Fetch-Site: same-origin" \
  -H "X-KBase-CSRF: invalid-${csrf_token}" \
  "${proxy_base}/api/browser/session/logout")"
if [[ "${invalid_csrf_status}" != "403" ]] ||
  [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
    -H "Cookie: ${session_cookie}" \
    "${proxy_base}/api/books")" != "200" ]]; then
  echo "invalid-CSRF logout was not rejected without revoking the session" >&2
  exit 1
fi

curl -sS -D "${temporary}/logout.headers" \
  -o "${temporary}/logout.body" \
  -X POST \
  -H "Cookie: ${session_cookie}" \
  -H "Origin: ${public_origin}" \
  -H "Sec-Fetch-Site: same-origin" \
  -H "X-KBase-CSRF: ${csrf_token}" \
  "${proxy_base}/api/browser/session/logout"
if [[ "$(response_status "${temporary}/logout.headers")" != "204" ]] ||
  ! grep -Eiq '^Set-Cookie:[[:space:]]*__Host-kbase_session=;[[:space:]]*Path=/;.*Max-Age=0;.*HttpOnly;[[:space:]]*Secure;[[:space:]]*SameSite=Strict' \
    "${temporary}/logout.headers" ||
  grep -Eiq '^Set-Cookie:.*;[[:space:]]*Domain=' "${temporary}/logout.headers"; then
  echo "CSRF-protected browser logout did not clear the Cookie" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Cookie: ${session_cookie}" \
  "${proxy_base}/api/books")" != "401" ]]; then
  echo "logged-out browser Cookie remained valid" >&2
  exit 1
fi

curl -sS -D "${temporary}/migration-epoch.headers" \
  -o "${temporary}/migration-epoch.body" \
  -u "${basic_user}:${basic_password}" \
  -H "X-KBase-Browser-Client-ID: ${second_client_id}" \
  "${proxy_base}/browser/session"
if [[ "$(response_status "${temporary}/migration-epoch.headers")" != "200" ]]; then
  echo "migration client epoch acquisition failed" >&2
  exit 1
fi
migration_epoch="$(json_number epoch "${temporary}/migration-epoch.body")"
if [[ -z "${migration_epoch}" ]]; then
  echo "migration client epoch was missing" >&2
  exit 1
fi

curl -sS -D "${temporary}/migration-origin.headers" \
  -o "${temporary}/migration-origin.body" \
  -X POST \
  -H "Authorization: Bearer ${api_value}" \
  -H "Origin: http://invalid.example.test" \
  -H "X-KBase-Browser-Client-ID: ${second_client_id}" \
  -H "X-KBase-Browser-Epoch: ${migration_epoch}" \
  "${proxy_base}/browser/session/migrate"
if [[ "$(response_status "${temporary}/migration-origin.headers")" != "403" ]] ||
  grep -Eiq '^WWW-Authenticate:[[:space:]]*Basic' "${temporary}/migration-origin.headers"; then
  echo "migration was not protected by the backend Origin policy" >&2
  exit 1
fi

curl -sS -D "${temporary}/migration-auth.headers" \
  -o "${temporary}/migration-auth.body" \
  -X POST \
  -H "Origin: ${public_origin}" \
  -H "X-KBase-Browser-Client-ID: ${second_client_id}" \
  -H "X-KBase-Browser-Epoch: ${migration_epoch}" \
  "${proxy_base}/browser/session/migrate"
if [[ "$(response_status "${temporary}/migration-auth.headers")" != "401" ]] ||
  grep -Eiq '^WWW-Authenticate:[[:space:]]*Basic' "${temporary}/migration-auth.headers"; then
  echo "migration was not protected by backend Bearer authentication" >&2
  exit 1
fi

curl -sS -D "${temporary}/migration.headers" \
  -o "${temporary}/migration.body" \
  -X POST \
  -H "Authorization: Bearer ${api_value}" \
  -H "Origin: ${public_origin}" \
  -H "X-KBase-Browser-Client-ID: ${second_client_id}" \
  -H "X-KBase-Browser-Epoch: ${migration_epoch}" \
  "${proxy_base}/browser/session/migrate"
if [[ "$(response_status "${temporary}/migration.headers")" != "200" ]] ||
  ! grep -Eiq '^Set-Cookie:[[:space:]]*__Host-kbase_session=[^;]+;[[:space:]]*Path=/;.*HttpOnly;[[:space:]]*Secure;[[:space:]]*SameSite=Strict' \
    "${temporary}/migration.headers" ||
  grep -Eiq '^Set-Cookie:.*;[[:space:]]*Domain=' "${temporary}/migration.headers"; then
  echo "Bearer migration did not create a secure Cookie" >&2
  exit 1
fi
if grep -Fq "${api_value}" "${temporary}/migration.body" ||
  grep -Eq '"(api_)?token"[[:space:]]*:' "${temporary}/migration.body"; then
  echo "Bearer migration leaked an API token" >&2
  exit 1
fi
migration_cookie="$(response_cookie "${temporary}/migration.headers")"
if [[ -z "${migration_cookie}" ]] ||
  [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
    -H "Cookie: ${migration_cookie}" \
    "${proxy_base}/api/books")" != "200" ]]; then
  echo "migrated browser Cookie did not authorize the API" >&2
  exit 1
fi

if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer ${api_value}" \
  "${proxy_base}/api/books")" != "200" ]]; then
  echo "machine Bearer authentication regressed" >&2
  exit 1
fi

curl -sS -D "${temporary}/retired.headers" \
  -o "${temporary}/retired.body" \
  "${proxy_base}/browser/session-token"
if [[ "$(response_status "${temporary}/retired.headers")" != "410" ]] ||
  grep -Eiq '^WWW-Authenticate:[[:space:]]*Basic' "${temporary}/retired.headers"; then
  echo "retired browser token endpoint was not handled authoritatively by the backend" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -u "${basic_user}:wrong" \
  "${proxy_base}/browser/session-token")" != "410" ]]; then
  echo "retired browser token endpoint still depends on Basic Auth" >&2
  exit 1
fi

if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  -H 'X-KBase-Browser-Session: 1' \
  "${backend_base}/browser/session")" != "401" ]]; then
  echo "backend accepted the historical fixed browser header" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  -H "X-KBase-Browser-Session: forged-$(openssl rand -hex 16)" \
  "${backend_base}/browser/session")" != "401" ]]; then
  echo "backend accepted a forged browser proxy header" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Basic forged" \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  -H "X-KBase-Browser-Session: ${browser_proxy_value}" \
  "${backend_base}/browser/session")" != "401" ]]; then
  echo "backend accepted forwarded Basic Authorization" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "X-KBase-Browser-Client-ID: ${client_id}" \
  -H "X-KBase-Browser-Session: ${browser_proxy_value}" \
  "${proxy_base}/browser/session")" != "401" ]]; then
  echo "Nginx accepted a forged browser proxy header without Basic Auth" >&2
  exit 1
fi

echo "browser session proxy smoke passed"
