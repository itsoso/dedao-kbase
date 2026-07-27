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
    "${nginx_bin}" -p "${temporary}/nginx-prefix" -c "${temporary}/nginx.conf" -s quit >/dev/null 2>&1 || true
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
  "${temporary}/web"
printf '%s\n' '<!doctype html><title>KBase smoke</title>' >"${temporary}/web/index.html"

api_value="$(openssl rand -hex 32)"
browser_proxy_value="$(openssl rand -hex 32)"
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

cat >"${temporary}/nginx.conf" <<EOF
worker_processes 1;
pid ${temporary}/nginx.pid;
error_log ${temporary}/nginx-error.log;
events { worker_connections 64; }
http {
    access_log off;
    server {
        listen 127.0.0.1:${proxy_port};
        server_name localhost;
        include ${temporary}/locations.conf;
    }
}
EOF
"${nginx_bin}" -p "${temporary}/nginx-prefix" -c "${temporary}/nginx.conf" -t >/dev/null
"${nginx_bin}" -p "${temporary}/nginx-prefix" -c "${temporary}/nginx.conf"
nginx_started=1

proxy_base="http://127.0.0.1:${proxy_port}"
curl -fsS "${proxy_base}/" >/dev/null
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' "${proxy_base}/browser/session-token")" != "401" ]]; then
  echo "browser exchange without Basic Auth did not return 401" >&2
  exit 1
fi
wrong_basic_status="$(curl -sS -o /dev/null -w '%{http_code}' -u "${basic_user}:wrong" "${proxy_base}/browser/session-token")"
if [[ "${wrong_basic_status}" != "401" ]]; then
  echo "browser exchange with invalid Basic Auth returned HTTP ${wrong_basic_status}, want 401" >&2
  exit 1
fi
session_json="$(curl -fsS -u "${basic_user}:${basic_password}" "${proxy_base}/browser/session-token")"
session_value="$(printf '%s' "${session_json}" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
if [[ -z "${session_value}" || "${session_value}" != "${api_value}" ]]; then
  echo "browser exchange did not return the expected bearer token" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' "${proxy_base}/api/books")" != "401" ]]; then
  echo "API without bearer token did not return 401" >&2
  exit 1
fi
curl -fsS -H "Authorization: Bearer ${session_value}" "${proxy_base}/api/books" >/dev/null

backend_base="http://127.0.0.1:${backend_port}"
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' -H 'X-KBase-Browser-Session: 1' "${backend_base}/browser/session-token")" != "401" ]]; then
  echo "backend accepted the historical fixed browser header" >&2
  exit 1
fi
if [[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Basic forged" -H "X-KBase-Browser-Session: ${browser_proxy_value}" "${backend_base}/browser/session-token")" != "401" ]]; then
  echo "backend accepted forwarded Basic Authorization" >&2
  exit 1
fi
curl -fsS -H "X-KBase-Browser-Session: ${browser_proxy_value}" "${backend_base}/browser/session-token" >/dev/null

echo "browser session proxy smoke passed"
