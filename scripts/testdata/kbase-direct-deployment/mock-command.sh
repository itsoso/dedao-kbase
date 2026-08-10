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

fail_once() {
  local operation="$1"
  local failure="${MOCK_SYSTEMCTL_STATE:?}/fail-${operation}-once"
  if [[ -f "$failure" ]]; then
    mv "$failure" "${failure}.used"
    return 0
  fi
  return 1
}

case "$command_name" in
  sudo)
    log_action "$@"
    preserve_list=""
    while [[ "${1:-}" == --preserve-env=* ]]; do
      preserve_list="${1#--preserve-env=}"
      shift
    done
    if [[ -n "$preserve_list" ]]; then
      while IFS='=' read -r environment_name _; do
        case "$environment_name" in
          KBASE_*)
            case ",${preserve_list}," in
              *",${environment_name},"*) ;;
              *) unset "$environment_name" ;;
            esac
            ;;
        esac
      done < <(env)
      [[ -z "${KBASE_UNLISTED_SENTINEL:-}" ]] || exit 9
    fi
    exec "$@"
    ;;
  systemctl)
    log_action "$@"
    action="${1:-}"
    service="${2:-}"
    case "$action" in
      daemon-reload)
        if [[ -f "${KBASE_WORKER_UNIT_TARGET:?}" ]] && fail_once daemon-reload-after-unit; then
          exit 1
        fi
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
        if fail_once "${action}-${service}"; then
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
          rm -f \
            "${MOCK_SYSTEMCTL_STATE:?}/${service}.enabled" \
            "${MOCK_SYSTEMCTL_STATE:?}/${service}.wants-symlink"
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
    argument_count="${#install_arguments[@]}"
    if [[ "$argument_count" -ge 2 ]]; then
      source_path="${install_arguments[$((argument_count - 2))]}"
      target_path="${install_arguments[$((argument_count - 1))]}"
      if [[ "$source_path" == "${KBASE_CANDIDATE_BIN:?}" ]] &&
        [[ "$target_path" == "${KBASE_BINARY_CANDIDATE_TARGET:?}" ]] &&
        fail_once stage-server-install; then
        exit 1
      fi
      if [[ "$source_path" == "${KBASE_WORKER_CANDIDATE_BIN:?}" ]] &&
        [[ "$target_path" == "${KBASE_WORKER_BINARY_CANDIDATE_TARGET:?}" ]] &&
        fail_once stage-worker-install; then
        exit 1
      fi
    fi
    exec /usr/bin/install "${install_arguments[@]}"
    ;;
  mv)
    log_action "$@"
    operation=""
    if [[ "${1:-}" == "${KBASE_BINARY_CANDIDATE_TARGET:?}" && "${2:-}" == "${KBASE_BINARY_TARGET:?}" ]]; then
      operation="server-move"
    elif [[ "${1:-}" == "${KBASE_WORKER_BINARY_CANDIDATE_TARGET:?}" && "${2:-}" == "${KBASE_WORKER_BINARY_TARGET:?}" ]]; then
      operation="worker-move"
    elif [[ "${1:-}" == "${KBASE_WEB_TARGET:?}" && "${2:-}" == "${KBASE_WEB_PREVIOUS_TARGET:?}" ]]; then
      operation="web-old-move"
    elif [[ "${1:-}" == "${KBASE_WEB_CANDIDATE_TARGET:?}" && "${2:-}" == "${KBASE_WEB_TARGET:?}" ]]; then
      operation="web-new-move"
    elif [[ "${1:-}" == "${KBASE_WORKER_UNIT_CANDIDATE_TARGET:?}" && "${2:-}" == "${KBASE_WORKER_UNIT_TARGET:?}" ]]; then
      operation="unit-move"
    fi
    if [[ -n "$operation" ]] && fail_once "$operation"; then
      exit 1
    fi
    exec /bin/mv "$@"
    ;;
  cp)
    log_action "$@"
    source_path="${2:-}"
    target_path="${3:-}"
    if [[ "$source_path" == "${KBASE_WEB_CANDIDATE_SOURCE:?}" ]] &&
      [[ "$target_path" == "${KBASE_WEB_CANDIDATE_TARGET:?}" ]] &&
      fail_once stage-web-copy; then
      exit 1
    fi
    exec /bin/cp "$@"
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
