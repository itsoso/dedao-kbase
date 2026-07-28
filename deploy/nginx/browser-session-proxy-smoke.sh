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
backend_pid=""
nginx_started=0
cleanup() {
  if [[ "${nginx_started}" -eq 1 ]]; then
    "${nginx_bin}" \
      -p "${temporary}/nginx-prefix" \
      -c "${temporary}/nginx.conf" \
      -e "${temporary}/nginx-error.log" \
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
  -e "${temporary}/nginx-error.log" \
  -t >/dev/null
"${nginx_bin}" \
  -p "${temporary}/nginx-prefix" \
  -c "${temporary}/nginx.conf" \
  -e "${temporary}/nginx-error.log"
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
if ! grep -Eiq '^Set-Cookie:[[:space:]]*__Host-kbase_session=[^;]+;.*HttpOnly;[[:space:]]*Secure;[[:space:]]*SameSite=Strict' \
  "${temporary}/login.headers"; then
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

curl -sS -D "${temporary}/logout.headers" \
  -o "${temporary}/logout.body" \
  -X POST \
  -H "Cookie: ${session_cookie}" \
  -H "Origin: ${public_origin}" \
  -H "Sec-Fetch-Site: same-origin" \
  -H "X-KBase-CSRF: ${csrf_token}" \
  "${proxy_base}/api/browser/session/logout"
if [[ "$(response_status "${temporary}/logout.headers")" != "204" ]] ||
  ! grep -Eiq '^Set-Cookie:[[:space:]]*__Host-kbase_session=;.*Max-Age=0;.*HttpOnly;[[:space:]]*Secure;[[:space:]]*SameSite=Strict' \
    "${temporary}/logout.headers"; then
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
  ! grep -Eiq '^Set-Cookie:[[:space:]]*__Host-kbase_session=[^;]+;.*HttpOnly;[[:space:]]*Secure;[[:space:]]*SameSite=Strict' \
    "${temporary}/migration.headers"; then
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
