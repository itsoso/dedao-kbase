#!/usr/bin/env bash

set -Eeuo pipefail

command_name="$(basename "$0")"

log_action() {
  printf '%s' "$command_name" >>"${MOCK_ACTION_LOG:?}"
  for argument in "$@"; do
    printf ' %q' "$argument" >>"${MOCK_ACTION_LOG:?}"
  done
  printf '\n' >>"${MOCK_ACTION_LOG:?}"
}

case "$command_name" in
  sudo)
    log_action "$@"
    while [[ "${1:-}" == --preserve-env=* ]]; do
      shift
    done
    exec "$@"
    ;;
  systemctl)
    log_action "$@"
    action="${1:-}"
    service="${2:-}"
    case "$action" in
      daemon-reload)
        exit 0
        ;;
      is-active)
        [[ "${2:-}" == "--quiet" ]] && service="${3:-}"
        test -f "${MOCK_SYSTEMCTL_STATE:?}/${service}.active"
        ;;
      is-enabled)
        [[ "${2:-}" == "--quiet" ]] && service="${3:-}"
        test -f "${MOCK_SYSTEMCTL_STATE:?}/${service}.enabled"
        ;;
      stop)
        if [[ "$service" == "${KBASE_WORKER_SERVICE_NAME:?}" ]] &&
          [[ ! -f "${KBASE_WORKER_UNIT_TARGET:?}" ]]; then
          exit 5
        fi
        rm -f "${MOCK_SYSTEMCTL_STATE:?}/${service}.active"
        ;;
      start|restart)
        failure="${MOCK_SYSTEMCTL_STATE:?}/fail-${action}-${service}-once"
        if [[ -f "$failure" ]]; then
          mv "$failure" "${failure}.used"
          exit 1
        fi
        if [[ "$service" == "${KBASE_WORKER_SERVICE_NAME:?}" ]] &&
          [[ ! -f "${KBASE_WORKER_UNIT_TARGET:?}" ]]; then
          exit 5
        fi
        touch "${MOCK_SYSTEMCTL_STATE:?}/${service}.active"
        ;;
      enable)
        touch "${MOCK_SYSTEMCTL_STATE:?}/${service}.enabled"
        touch "${MOCK_SYSTEMCTL_STATE:?}/${service}.wants-symlink"
        ;;
      disable)
        if [[ "$service" == "${KBASE_WORKER_SERVICE_NAME:?}" ]] &&
          [[ ! -f "${KBASE_WORKER_UNIT_TARGET:?}" ]]; then
          exit 5
        fi
        rm -f \
          "${MOCK_SYSTEMCTL_STATE:?}/${service}.enabled" \
          "${MOCK_SYSTEMCTL_STATE:?}/${service}.wants-symlink"
        ;;
      *)
        printf 'unsupported mock systemctl action: %s\n' "$action" >&2
        exit 2
        ;;
    esac
    ;;
  runuser)
    log_action "$@"
    [[ "${1:-}" == "--user" ]] || exit 2
    shift 2
    [[ "${1:-}" == "--" ]] || exit 2
    shift
    exec "$@"
    ;;
  curl)
    log_action "$@"
    exit 0
    ;;
  sqlite3)
    log_action "$@"
    database="${1:?}"
    backup_command="${2:?}"
    backup_path="$(printf '%s' "$backup_command" | sed -n "s/^\.backup '\(.*\)'$/\1/p")"
    [[ -n "$backup_path" ]] || exit 2
    cp "$database" "$backup_path"
    ;;
  install)
    log_action "$@"
    install_arguments=()
    while [[ "$#" -gt 0 ]]; do
      case "$1" in
        -o|-g)
          shift 2
          ;;
        *)
          install_arguments+=("$1")
          shift
          ;;
      esac
    done
    exec /usr/bin/install "${install_arguments[@]}"
    ;;
  book-job-worker)
    log_action "$@"
    [[ "${1:-}" == "export-legacy" && "${2:-}" == "--out" && -n "${3:-}" ]] || exit 2
    mkdir -p "${KBASE_BOOK_KNOWLEDGE_ROOT:?}"
    if [[ ! -f "${KBASE_BOOK_JOBS_DB:?}" ]]; then
      printf 'sqlite-created-by-export\n' >"${KBASE_BOOK_JOBS_DB:?}"
    fi
    printf '{"jobs":[]}\n' >"${3:?}"
    ;;
  *)
    printf 'unsupported mock command: %s\n' "$command_name" >&2
    exit 2
    ;;
esac
