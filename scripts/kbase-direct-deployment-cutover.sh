#!/usr/bin/env bash

set -Eeuo pipefail

fail() {
  printf 'kbase direct deployment cutover: %s\n' "$*" >&2
  exit 1
}

require_variable() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "${name} is required"
}

for variable in \
  KBASE_BACKUP_DIR \
  KBASE_BINARY_TARGET \
  KBASE_WORKER_BINARY_TARGET \
  KBASE_WEB_TARGET \
  KBASE_BOOK_JOBS_DB \
  KBASE_LEGACY_JOBS_PATH \
  KBASE_WORKER_UNIT_TARGET \
  KBASE_CANDIDATE_BIN \
  KBASE_WORKER_CANDIDATE_BIN \
  KBASE_WEB_CANDIDATE_SOURCE \
  KBASE_WORKER_UNIT_CANDIDATE_SOURCE \
  KBASE_BINARY_CANDIDATE_TARGET \
  KBASE_WORKER_BINARY_CANDIDATE_TARGET \
  KBASE_WEB_CANDIDATE_TARGET \
  KBASE_WEB_PREVIOUS_TARGET \
  KBASE_FAILED_WEB_TARGET \
  KBASE_WORKER_UNIT_CANDIDATE_TARGET \
  KBASE_SERVER_SHA256 \
  KBASE_WORKER_SHA256 \
  KBASE_SERVICE_NAME \
  KBASE_WORKER_SERVICE_NAME \
  KBASE_LOOPBACK_HEALTH_URL \
  KBASE_BOOK_KNOWLEDGE_ROOT \
  KBASE_SERVICE_USER
do
  require_variable "$variable"
done

test ! -e "${KBASE_BACKUP_DIR:?}" || fail "backup directory already exists"
test ! -e "${KBASE_BINARY_CANDIDATE_TARGET:?}" || fail "server staging target already exists"
test ! -e "${KBASE_WORKER_BINARY_CANDIDATE_TARGET:?}" || fail "Worker staging target already exists"
test ! -e "${KBASE_WEB_CANDIDATE_TARGET:?}" || fail "Web staging target already exists"
test ! -e "${KBASE_WEB_PREVIOUS_TARGET:?}" || fail "previous Web target already exists"
test ! -e "${KBASE_FAILED_WEB_TARGET:?}" || fail "failed Web target already exists"
test ! -e "${KBASE_WORKER_UNIT_CANDIDATE_TARGET:?}" || fail "unit staging target already exists"
test -f "${KBASE_BINARY_TARGET:?}" || fail "installed KBase binary is missing"
test -d "${KBASE_WEB_TARGET:?}" || fail "installed Web directory is missing"
test -f "${KBASE_CANDIDATE_BIN:?}" || fail "candidate KBase binary is missing"
test -f "${KBASE_WORKER_CANDIDATE_BIN:?}" || fail "candidate Worker binary is missing"
test -d "${KBASE_WEB_CANDIDATE_SOURCE:?}" || fail "candidate Web directory is missing"
test -f "${KBASE_WORKER_UNIT_CANDIDATE_SOURCE:?}" || fail "candidate Worker unit is missing"

server_hash="$(sha256sum "${KBASE_CANDIDATE_BIN:?}" | awk '{print $1}')"
worker_hash="$(sha256sum "${KBASE_WORKER_CANDIDATE_BIN:?}" | awk '{print $1}')"
[[ "$server_hash" == "${KBASE_SERVER_SHA256:?}" ]] || fail "candidate KBase SHA-256 mismatch"
[[ "$worker_hash" == "${KBASE_WORKER_SHA256:?}" ]] || fail "candidate Worker SHA-256 mismatch"

sudo install -d -o root -g root -m 0700 "${KBASE_BACKUP_DIR:?}"
sudo install -o root -g root -m 0755 \
  "${KBASE_BINARY_TARGET:?}" \
  "${KBASE_BACKUP_DIR:?}/kbase-server"
sudo cp -a "${KBASE_WEB_TARGET:?}" "${KBASE_BACKUP_DIR:?}/frontend-web"

if test -f "${KBASE_WORKER_BINARY_TARGET:?}"; then
  sudo install -o root -g root -m 0755 \
    "${KBASE_WORKER_BINARY_TARGET:?}" \
    "${KBASE_BACKUP_DIR:?}/book-job-worker"
  sudo touch "${KBASE_BACKUP_DIR:?}/book-job-worker.present"
else
  sudo touch "${KBASE_BACKUP_DIR:?}/book-job-worker.absent"
fi

if test -f "${KBASE_WORKER_UNIT_TARGET:?}"; then
  sudo install -o root -g root -m 0644 \
    "${KBASE_WORKER_UNIT_TARGET:?}" \
    "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service"
  sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.present"
  if sudo systemctl is-enabled --quiet "${KBASE_WORKER_SERVICE_NAME:?}"; then
    sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.enabled"
  else
    sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.disabled"
  fi
  if sudo systemctl is-active --quiet "${KBASE_WORKER_SERVICE_NAME:?}"; then
    sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.active"
  else
    sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.inactive"
  fi
else
  sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.absent"
  sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.disabled"
  sudo touch "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.inactive"
fi

if test -f "${KBASE_BOOK_JOBS_DB:?}"; then
  sudo sqlite3 "${KBASE_BOOK_JOBS_DB:?}" \
    ".backup '${KBASE_BACKUP_DIR:?}/book_jobs.sqlite3'"
  sudo chmod 0600 "${KBASE_BACKUP_DIR:?}/book_jobs.sqlite3"
  sudo touch "${KBASE_BACKUP_DIR:?}/book_jobs.sqlite3.present"
else
  sudo touch "${KBASE_BACKUP_DIR:?}/book_jobs.sqlite3.absent"
fi

if test -f "${KBASE_LEGACY_JOBS_PATH:?}"; then
  sudo install -o root -g root -m 0600 \
    "${KBASE_LEGACY_JOBS_PATH:?}" \
    "${KBASE_BACKUP_DIR:?}/jobs.json"
  sudo touch "${KBASE_BACKUP_DIR:?}/jobs.json.present"
else
  sudo touch "${KBASE_BACKUP_DIR:?}/jobs.json.absent"
fi

sudo install -o root -g root -m 0755 \
  "${KBASE_CANDIDATE_BIN:?}" \
  "${KBASE_BINARY_CANDIDATE_TARGET:?}"
sudo install -o root -g root -m 0755 \
  "${KBASE_WORKER_CANDIDATE_BIN:?}" \
  "${KBASE_WORKER_BINARY_CANDIDATE_TARGET:?}"
sudo cp -a "${KBASE_WEB_CANDIDATE_SOURCE:?}" "${KBASE_WEB_CANDIDATE_TARGET:?}"
sudo install -o root -g root -m 0644 \
  "${KBASE_WORKER_UNIT_CANDIDATE_SOURCE:?}" \
  "${KBASE_WORKER_UNIT_CANDIDATE_TARGET:?}"

test "$(sha256sum "${KBASE_BINARY_CANDIDATE_TARGET:?}" | awk '{print $1}')" = \
  "${KBASE_SERVER_SHA256:?}"
test "$(sha256sum "${KBASE_WORKER_BINARY_CANDIDATE_TARGET:?}" | awk '{print $1}')" = \
  "${KBASE_WORKER_SHA256:?}"

KBASE_LEGACY_JOBS_TEMP="${KBASE_LEGACY_JOBS_PATH:?}.rollback.$$"

rollback_direct_deployment() {
  local status=$?
  trap - ERR

  if ! sudo systemctl stop "${KBASE_WORKER_SERVICE_NAME:?}"; then
    sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.absent" || exit "$status"
  fi
  sudo systemctl stop "${KBASE_SERVICE_NAME:?}" || exit "$status"

  if ! sudo runuser --user "${KBASE_SERVICE_USER:?}" -- env \
    KBASE_BOOK_KNOWLEDGE_ROOT="${KBASE_BOOK_KNOWLEDGE_ROOT:?}" \
    KBASE_BOOK_JOBS_DB="${KBASE_BOOK_JOBS_DB:?}" \
    "${KBASE_WORKER_CANDIDATE_BIN:?}" \
    export-legacy --out "${KBASE_LEGACY_JOBS_TEMP:?}"
  then
    printf 'kbase direct deployment cutover: rollback legacy export failed\n' >&2
    exit "$status"
  fi
  sudo mv "${KBASE_LEGACY_JOBS_TEMP:?}" "${KBASE_LEGACY_JOBS_PATH:?}"

  sudo systemctl disable "${KBASE_WORKER_SERVICE_NAME:?}"
  sudo install -o root -g root -m 0755 \
    "${KBASE_BACKUP_DIR:?}/kbase-server" \
    "${KBASE_BINARY_TARGET:?}"
  if sudo test -f "${KBASE_BACKUP_DIR:?}/book-job-worker.present"; then
    sudo install -o root -g root -m 0755 \
      "${KBASE_BACKUP_DIR:?}/book-job-worker" \
      "${KBASE_WORKER_BINARY_TARGET:?}"
  else
    sudo rm -f "${KBASE_WORKER_BINARY_TARGET:?}"
  fi
  if test -e "${KBASE_WEB_TARGET:?}"; then
    sudo mv "${KBASE_WEB_TARGET:?}" "${KBASE_FAILED_WEB_TARGET:?}"
  fi
  sudo cp -a "${KBASE_BACKUP_DIR:?}/frontend-web" "${KBASE_WEB_TARGET:?}"
  if sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.present"; then
    sudo install -o root -g root -m 0644 \
      "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service" \
      "${KBASE_WORKER_UNIT_TARGET:?}"
  else
    sudo rm -f "${KBASE_WORKER_UNIT_TARGET:?}"
  fi
  sudo systemctl daemon-reload

  if sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.present"; then
    if sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.enabled"; then
      sudo systemctl enable "${KBASE_WORKER_SERVICE_NAME:?}"
    else
      sudo systemctl disable "${KBASE_WORKER_SERVICE_NAME:?}"
    fi
  fi
  sudo systemctl restart "${KBASE_SERVICE_NAME:?}"
  curl --fail --silent --show-error "${KBASE_LOOPBACK_HEALTH_URL:?}"
  sudo systemctl is-active --quiet "${KBASE_SERVICE_NAME:?}"
  if sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.active"; then
    sudo systemctl start "${KBASE_WORKER_SERVICE_NAME:?}"
    sudo systemctl is-active --quiet "${KBASE_WORKER_SERVICE_NAME:?}"
  else
    sudo systemctl stop "${KBASE_WORKER_SERVICE_NAME:?}" ||
      sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.absent"
  fi
  exit "$status"
}

trap rollback_direct_deployment ERR
sudo systemctl stop "${KBASE_WORKER_SERVICE_NAME:?}" || \
  sudo test -f "${KBASE_BACKUP_DIR:?}/dedao-book-job-worker.service.absent"
sudo systemctl stop "${KBASE_SERVICE_NAME:?}"
sudo mv "${KBASE_BINARY_CANDIDATE_TARGET:?}" "${KBASE_BINARY_TARGET:?}"
sudo mv "${KBASE_WORKER_BINARY_CANDIDATE_TARGET:?}" "${KBASE_WORKER_BINARY_TARGET:?}"
sudo mv "${KBASE_WEB_TARGET:?}" "${KBASE_WEB_PREVIOUS_TARGET:?}"
sudo mv "${KBASE_WEB_CANDIDATE_TARGET:?}" "${KBASE_WEB_TARGET:?}"
sudo mv "${KBASE_WORKER_UNIT_CANDIDATE_TARGET:?}" "${KBASE_WORKER_UNIT_TARGET:?}"
sudo systemctl daemon-reload
sudo systemctl start "${KBASE_SERVICE_NAME:?}"
sudo systemctl enable "${KBASE_WORKER_SERVICE_NAME:?}"
sudo systemctl start "${KBASE_WORKER_SERVICE_NAME:?}"
curl --fail --silent --show-error "${KBASE_LOOPBACK_HEALTH_URL:?}"
sudo systemctl is-active --quiet "${KBASE_SERVICE_NAME:?}"
sudo systemctl is-enabled --quiet "${KBASE_WORKER_SERVICE_NAME:?}"
sudo systemctl is-active --quiet "${KBASE_WORKER_SERVICE_NAME:?}"
trap - ERR
