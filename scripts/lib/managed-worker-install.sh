#!/usr/bin/env bash

# HOME-scoped transaction for the shared managed-worker Keychain account,
# launchd state, plist, and worker/updater pair. Bash 3.2 compatible.

MANAGED_WORKER_INSTALL_ACTIVE=false
MANAGED_WORKER_INSTALL_LOCK_HELD=false
MANAGED_WORKER_INSTALL_HOME=""
MANAGED_WORKER_INSTALL_ROOT=""
MANAGED_WORKER_INSTALL_LOCK=""
MANAGED_WORKER_INSTALL_LOCK_HELPER_PID=""
MANAGED_WORKER_INSTALL_LOCK_READY=""
MANAGED_WORKER_INSTALL_LOCK_RELEASE=""
MANAGED_WORKER_LIFECYCLE_LOCK_HELD=false
MANAGED_WORKER_LIFECYCLE_HELPER_PID=""
MANAGED_WORKER_LIFECYCLE_DIRECTORY=""
MANAGED_WORKER_LIFECYCLE_READY=""
MANAGED_WORKER_LIFECYCLE_RELEASE=""
MANAGED_WORKER_LIFECYCLE_MAINTENANCE=""
MANAGED_WORKER_LIFECYCLE_HOLDER=""
MANAGED_WORKER_LIFECYCLE_WORKER_TYPE=""
MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN=false
MANAGED_WORKER_INSTALL_JOURNAL=""
MANAGED_WORKER_INSTALL_JOURNAL_TMP=""
MANAGED_WORKER_INSTALL_PLIST_BACKUP=""
MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP=""
MANAGED_WORKER_INSTALL_CONFIG_BACKUP=""
MANAGED_WORKER_INSTALL_WORKER=""
MANAGED_WORKER_INSTALL_UPDATER=""
MANAGED_WORKER_INSTALL_PLIST=""
MANAGED_WORKER_INSTALL_UPDATER_PLIST=""
MANAGED_WORKER_INSTALL_CONFIG=""
MANAGED_WORKER_INSTALL_DOMAIN=""
MANAGED_WORKER_INSTALL_LABEL=""
MANAGED_WORKER_INSTALL_UPDATER_LABEL=""
MANAGED_WORKER_INSTALL_KIND=""
MANAGED_WORKER_INSTALL_PHASE=""
MANAGED_WORKER_INSTALL_PLIST_OLD=0
MANAGED_WORKER_INSTALL_PLIST_OLD_HASH="-"
MANAGED_WORKER_INSTALL_PLIST_OLD_MODE="-"
MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD=0
MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH="-"
MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE="-"
MANAGED_WORKER_INSTALL_CONFIG_OLD=0
MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH="-"
MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE="-"
MANAGED_WORKER_INSTALL_KEYCHAIN_OLD=0
MANAGED_WORKER_INSTALL_SERVICE_LOADED=0
MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED=0
MANAGED_WORKER_INSTALL_KEYCHAIN_SERVICE="life.executor.kbase.source-agent"
MANAGED_WORKER_INSTALL_KEYCHAIN_ACCOUNT="transport-token"
MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT="transport-token-install-backup"
unset MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE
MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""

_managed_worker_install_error() {
  printf '%s\n' "managed worker installation transaction failed" >&2
}

_managed_worker_install_rollback_error() {
  printf '%s\n' "managed worker installation rollback failed" >&2
}

_managed_worker_install_security() {
  /usr/bin/security "$@"
}

_managed_worker_install_launchctl() {
  /bin/launchctl "$@"
}

_managed_worker_install_probe_service() {
  local target="$1" status=0
  if _managed_worker_install_launchctl print "$target" >/dev/null 2>&1; then
    return 0
  else
    status=$?
  fi
  if [[ $status -eq 113 ]]; then return 1; fi
  return 2
}

_managed_worker_install_sync() {
  sync
}

_managed_worker_lifecycle_abort_acquire() {
  local helper_pid="$MANAGED_WORKER_LIFECYCLE_HELPER_PID" attempt=0
  exec 6>&- 7<&- 2>/dev/null || true
  if [[ -n "$helper_pid" ]]; then
    while kill -0 "$helper_pid" 2>/dev/null && ((attempt < 500)); do /bin/sleep 0.01; attempt=$((attempt + 1)); done
    if kill -0 "$helper_pid" 2>/dev/null; then kill -KILL "$helper_pid" 2>/dev/null || true; fi
    wait "$helper_pid" 2>/dev/null || true
  fi
  rm -f "$MANAGED_WORKER_LIFECYCLE_READY" "$MANAGED_WORKER_LIFECYCLE_RELEASE" 2>/dev/null || true
  MANAGED_WORKER_LIFECYCLE_LOCK_HELD=false
  MANAGED_WORKER_LIFECYCLE_HELPER_PID=""
  return 1
}

_managed_worker_lifecycle_acquire_once() {
  local directory="$1" holder="$2" worker_type="$3" canonical mode owner helper_pid acknowledgement
  if [[ "$MANAGED_WORKER_LIFECYCLE_LOCK_HELD" == true || -n "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" ||
    ! -d "$directory" || -L "$directory" || ! -x "$holder" || -L "$holder" ]]; then return 1; fi
  canonical="$(cd "$directory" 2>/dev/null && pwd -P)" || return 1
  [[ "$canonical" == "$directory" ]] || return 1
  mode="$(stat -f '%Lp' "$directory" 2>/dev/null || true)"
  owner="$(stat -f '%u' "$directory" 2>/dev/null || true)"
  [[ "$mode" == 700 && "$owner" == "$(id -u)" ]] || return 1
  MANAGED_WORKER_LIFECYCLE_DIRECTORY="$directory"
  MANAGED_WORKER_LIFECYCLE_READY="$directory/.managed-worker-lifecycle-ready.$$.$RANDOM"
  MANAGED_WORKER_LIFECYCLE_RELEASE="$directory/.managed-worker-lifecycle-release.$$.$RANDOM"
  MANAGED_WORKER_LIFECYCLE_MAINTENANCE="$directory/.managed-worker-maintenance"
  rm -f "$MANAGED_WORKER_LIFECYCLE_READY" "$MANAGED_WORKER_LIFECYCLE_RELEASE" || return 1
  mkfifo -m 0600 "$MANAGED_WORKER_LIFECYCLE_READY" "$MANAGED_WORKER_LIFECYCLE_RELEASE" || return 1
  "$holder" --hold-lifecycle-lock --worker-type "$worker_type" \
    <"$MANAGED_WORKER_LIFECYCLE_RELEASE" >"$MANAGED_WORKER_LIFECYCLE_READY" 2>/dev/null &
  helper_pid=$!
  MANAGED_WORKER_LIFECYCLE_HELPER_PID="$helper_pid"
  exec 6>"$MANAGED_WORKER_LIFECYCLE_RELEASE"
  exec 7<"$MANAGED_WORKER_LIFECYCLE_READY"
  if ! IFS= read -r -t 5 acknowledgement <&7 || [[ "$acknowledgement" != locked ]]; then
    _managed_worker_lifecycle_abort_acquire
    return 1
  fi
  if [[ -L "$MANAGED_WORKER_LIFECYCLE_MAINTENANCE" || ! -f "$MANAGED_WORKER_LIFECYCLE_MAINTENANCE" ||
    "$(stat -f '%Lp' "$MANAGED_WORKER_LIFECYCLE_MAINTENANCE" 2>/dev/null || true)" != 600 ||
    "$(stat -f '%u' "$MANAGED_WORKER_LIFECYCLE_MAINTENANCE" 2>/dev/null || true)" != "$(id -u)" ]]; then
    _managed_worker_lifecycle_abort_acquire
    return 1
  fi
  case "$(<"$MANAGED_WORKER_LIFECYCLE_MAINTENANCE")" in
    initial) MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN=false ;;
    begin-mutation) MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN=true ;;
    *) _managed_worker_lifecycle_abort_acquire; return 1 ;;
  esac
  MANAGED_WORKER_LIFECYCLE_HOLDER="$holder"
  MANAGED_WORKER_LIFECYCLE_WORKER_TYPE="$worker_type"
  MANAGED_WORKER_LIFECYCLE_LOCK_HELD=true
}

_managed_worker_lifecycle_acquire() {
  local directory="$1" holder="$2" worker_type="$3" lock_attempt=0
  while ((lock_attempt < 500)); do
    if _managed_worker_lifecycle_acquire_once "$directory" "$holder" "$worker_type"; then return 0; fi
    /bin/sleep 0.01
    lock_attempt=$((lock_attempt + 1))
  done
  return 1
}

_managed_worker_lifecycle_message() {
  local message="$1" expected="$2" acknowledgement
  [[ "$MANAGED_WORKER_LIFECYCLE_LOCK_HELD" == true ]] || return 1
  printf '%s\n' "$message" >&6 || return 1
  IFS= read -r -t 5 acknowledgement <&7 || return 1
  [[ "$acknowledgement" == "$expected" ]] || return 1
  if [[ "$message" == begin-mutation ]]; then MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN=true; fi
}

managed_worker_lifecycle_assert_alive() {
  [[ "$MANAGED_WORKER_LIFECYCLE_LOCK_HELD" == true && -n "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" ]] &&
    kill -0 "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" 2>/dev/null
}

_managed_worker_lifecycle_refuse() {
  if [[ "$MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN" == true ]]; then
    _managed_worker_lifecycle_release false
  else
    _managed_worker_lifecycle_release true
  fi
}

_managed_worker_lifecycle_release() {
  local remove_marker="${1:-true}" status=0 attempt=0
  if [[ "$MANAGED_WORKER_LIFECYCLE_LOCK_HELD" != true ]]; then return 0; fi
  if [[ "$remove_marker" == true ]]; then
    if [[ "$MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN" == true ]]; then
      _managed_worker_lifecycle_message commit committed || status=1
    else
      _managed_worker_lifecycle_message abort-before-mutation aborted || status=1
    fi
  fi
  exec 6>&- 7<&- 2>/dev/null || status=1
  if [[ -n "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" ]]; then
    while kill -0 "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" 2>/dev/null && ((attempt < 500)); do
      /bin/sleep 0.01
      attempt=$((attempt + 1))
    done
    if kill -0 "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" 2>/dev/null; then
      kill -KILL "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" 2>/dev/null || true
      status=1
    fi
    wait "$MANAGED_WORKER_LIFECYCLE_HELPER_PID" 2>/dev/null || [[ $status -ne 0 ]] || status=1
  fi
  rm -f "$MANAGED_WORKER_LIFECYCLE_READY" "$MANAGED_WORKER_LIFECYCLE_RELEASE" 2>/dev/null || status=1
  MANAGED_WORKER_LIFECYCLE_LOCK_HELD=false
  MANAGED_WORKER_LIFECYCLE_HELPER_PID=""
  MANAGED_WORKER_LIFECYCLE_DIRECTORY=""
  MANAGED_WORKER_LIFECYCLE_MAINTENANCE=""
  MANAGED_WORKER_LIFECYCLE_HOLDER=""
  MANAGED_WORKER_LIFECYCLE_WORKER_TYPE=""
  MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN=false
  return "$status"
}

_managed_worker_install_hash() {
  _managed_worker_pair_hash "$1"
}

_managed_worker_install_valid_secret() {
  local value="$1" index
  if ((${#value} == 0 || ${#value} > 1024)); then return 1; fi
  for ((index = 0; index < ${#value}; index++)); do
    case "${value:index:1}" in
      [[:graph:]]) ;;
      *) return 1 ;;
    esac
  done
}

_managed_worker_install_read_keychain_value() {
  local account="$1" producer_pid read_status=0 producer_status=0 extra="" oversized=false
  local value_pipe="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-keychain-pipe.$$"
  unset MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE
  MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
  if [[ -L "$value_pipe" || (-e "$value_pipe" && ! -p "$value_pipe") ]]; then return 1; fi
  rm -f "$value_pipe" 2>/dev/null || return 1
  /usr/bin/mkfifo -m 0600 "$value_pipe" || return 1
  _managed_worker_install_security find-generic-password \
    -s "$MANAGED_WORKER_INSTALL_KEYCHAIN_SERVICE" -a "$account" -w >"$value_pipe" 2>/dev/null &
  producer_pid=$!
  exec 9<"$value_pipe"
  rm -f "$value_pipe" 2>/dev/null || {
    kill "$producer_pid" 2>/dev/null || true
    wait "$producer_pid" 2>/dev/null || true
    exec 9<&-
    return 1
  }
  IFS= read -r -n 1025 MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE <&9 || read_status=$?
  if ((${#MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE} > 1024)); then
    oversized=true
  elif IFS= read -r -n 1 extra <&9; then
    oversized=true
  fi
  exec 9<&-
  if [[ "$oversized" == true ]]; then kill "$producer_pid" 2>/dev/null || true; fi
  wait "$producer_pid" 2>/dev/null || producer_status=$?
  if [[ "$oversized" == true ]]; then
    MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
    return 65
  fi
  if [[ $producer_status -ne 0 ]]; then
    MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
    return "$producer_status"
  fi
  if [[ $read_status -ne 0 && -z "$MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE" ]]; then return 65; fi
  _managed_worker_install_valid_secret "$MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE" || {
    MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
    return 65
  }
}

_managed_worker_install_valid_field() {
  local value="$1"
  [[ -n "$value" && "$value" != *=* && "$value" != *[[:cntrl:]]* ]]
}

_managed_worker_install_canonical_target() {
  local path="$1" expected_basename="$2" parent basename canonical_parent
  _managed_worker_install_valid_field "$path" || return 1
  if [[ "$path" != /* ]]; then return 1; fi
  case "$path" in
    */../* | */.. | */./* | */.) return 1 ;;
  esac
  parent="${path%/*}"
  basename="${path##*/}"
  [[ "$basename" == "$expected_basename" && "$parent" != "$path" ]] || return 1
  canonical_parent="$(cd "$parent" 2>/dev/null && pwd -P)" || return 1
  case "$canonical_parent" in "$MANAGED_WORKER_INSTALL_HOME"/*) ;; *) return 1 ;; esac
  [[ "$path" == "$canonical_parent/$basename" ]]
}

_managed_worker_install_set_home_paths() {
  local home="$1" canonical_home
  _managed_worker_install_valid_field "$home" || return 1
  if [[ "$home" != /* ]]; then return 1; fi
  case "$home" in */../* | */.. | */./* | */.) return 1 ;; esac
  canonical_home="$(cd "$home" 2>/dev/null && pwd -P)" || return 1
  [[ "$home" == "$canonical_home" ]] || return 1
  MANAGED_WORKER_INSTALL_HOME="$canonical_home"
  MANAGED_WORKER_INSTALL_ROOT="$canonical_home/Library/Application Support/KBase"
  MANAGED_WORKER_INSTALL_LOCK="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock"
  MANAGED_WORKER_INSTALL_LOCK_READY="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-ready.$$"
  MANAGED_WORKER_INSTALL_LOCK_RELEASE="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install.lock-release.$$"
  MANAGED_WORKER_INSTALL_JOURNAL="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install-journal"
  MANAGED_WORKER_INSTALL_JOURNAL_TMP="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install-journal.tmp"
  MANAGED_WORKER_INSTALL_PLIST_BACKUP="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install-plist-old"
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install-updater-plist-old"
  MANAGED_WORKER_INSTALL_CONFIG_BACKUP="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-install-updater-config-old"
}

_managed_worker_install_prepare_root() {
  local path canonical
  for path in \
    "$MANAGED_WORKER_INSTALL_HOME/Library" \
    "$MANAGED_WORKER_INSTALL_HOME/Library/Application Support" \
    "$MANAGED_WORKER_INSTALL_ROOT"; do
    if [[ -L "$path" || (-e "$path" && ! -d "$path") ]]; then return 1; fi
    if [[ ! -d "$path" ]] && ! mkdir "$path"; then return 1; fi
    canonical="$(cd "$path" 2>/dev/null && pwd -P)" || return 1
    [[ "$canonical" == "$path" ]] || return 1
  done
}

_managed_worker_install_acquire_lock() {
  local attempt=0
  while ((attempt < 300)); do
    if _managed_worker_install_try_acquire_lock; then return 0; fi
    if [[ -L "$MANAGED_WORKER_INSTALL_LOCK" || (-e "$MANAGED_WORKER_INSTALL_LOCK" && ! -f "$MANAGED_WORKER_INSTALL_LOCK") ]]; then return 1; fi
    sleep 0.1
    attempt=$((attempt + 1))
  done
  return 1
}

_managed_worker_install_cleanup_lock_markers() {
  local marker suffix
  for marker in \
    "$MANAGED_WORKER_INSTALL_ROOT"/.managed-worker-install.lock-ready.* \
    "$MANAGED_WORKER_INSTALL_ROOT"/.managed-worker-install.lock-release.*; do
    if [[ ! -e "$marker" && ! -L "$marker" ]]; then continue; fi
    suffix="${marker##*.}"
    if [[ ! "$suffix" =~ ^[0123456789]+$ || -L "$marker" || ! -f "$marker" ]]; then return 1; fi
    rm -f "$marker" 2>/dev/null || return 1
  done
}

_managed_worker_install_write_lock_release() {
  if [[ -L "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" ||
    (-e "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" && ! -f "$MANAGED_WORKER_INSTALL_LOCK_RELEASE") ]]; then return 1; fi
  (umask 077; set -o noclobber; printf 'release\n' >"$MANAGED_WORKER_INSTALL_LOCK_RELEASE") 2>/dev/null
}

_managed_worker_install_try_acquire_lock() {
  local attempt=0 helper_pid
  if [[ ! -x /usr/bin/perl ]]; then
    printf '%s\n' "managed worker lock helper requires /usr/bin/perl" >&2
    return 1
  fi
  if [[ "$MANAGED_WORKER_INSTALL_LOCK_HELD" == true || -n "$MANAGED_WORKER_INSTALL_LOCK_HELPER_PID" ]]; then return 1; fi
  if [[ -L "$MANAGED_WORKER_INSTALL_LOCK" || (-e "$MANAGED_WORKER_INSTALL_LOCK" && ! -f "$MANAGED_WORKER_INSTALL_LOCK") ]]; then return 1; fi
  rm -f "$MANAGED_WORKER_INSTALL_LOCK_READY" "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" 2>/dev/null || return 1
  /usr/bin/env -i /usr/bin/perl -MFcntl=:DEFAULT,:flock -e '
    use strict;
    use warnings;
    my ($lock, $ready, $release, $owner) = @ARGV;
    exit 64 unless defined($owner) && $owner =~ /\A[0-9]+\z/;
    $SIG{HUP} = "IGNORE";
    $SIG{INT} = "IGNORE";
    $SIG{TERM} = "IGNORE";
    umask 0077;
    sysopen(my $lock_fh, $lock, O_WRONLY | O_CREAT | O_APPEND | O_NOFOLLOW, 0600) or exit 74;
    flock($lock_fh, LOCK_EX | LOCK_NB) or exit 75;
    END { unlink($ready); unlink($release); }
    sysopen(my $ready_fh, $ready, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600) or exit 73;
    close($ready_fh) or exit 74;
    while (getppid() == $owner && kill(0, $owner) && !lstat($release)) {
      select(undef, undef, undef, 0.05);
    }
  ' "$MANAGED_WORKER_INSTALL_LOCK" "$MANAGED_WORKER_INSTALL_LOCK_READY" "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" "$$" &
  helper_pid=$!
  MANAGED_WORKER_INSTALL_LOCK_HELPER_PID="$helper_pid"
  while ((attempt < 500)); do
    if [[ -f "$MANAGED_WORKER_INSTALL_LOCK_READY" && ! -L "$MANAGED_WORKER_INSTALL_LOCK_READY" ]]; then
      if ! rm -f "$MANAGED_WORKER_INSTALL_LOCK_READY" 2>/dev/null || ! _managed_worker_install_cleanup_lock_markers; then
        _managed_worker_install_write_lock_release || true
        wait "$helper_pid" 2>/dev/null || true
        MANAGED_WORKER_INSTALL_LOCK_HELPER_PID=""
        return 1
      fi
      MANAGED_WORKER_INSTALL_LOCK_HELD=true
      return 0
    fi
    if ! kill -0 "$helper_pid" 2>/dev/null; then
      wait "$helper_pid" 2>/dev/null || true
      rm -f "$MANAGED_WORKER_INSTALL_LOCK_READY" "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" 2>/dev/null || true
      MANAGED_WORKER_INSTALL_LOCK_HELPER_PID=""
      return 1
    fi
    /bin/sleep 0.01
    attempt=$((attempt + 1))
  done
  _managed_worker_install_write_lock_release || true
  wait "$helper_pid" 2>/dev/null || true
  rm -f "$MANAGED_WORKER_INSTALL_LOCK_READY" "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" 2>/dev/null || true
  MANAGED_WORKER_INSTALL_LOCK_HELPER_PID=""
  return 1
}

_managed_worker_install_reclaim_stale_lock() {
  if ! _managed_worker_install_try_acquire_lock; then return 1; fi
  _managed_worker_install_release_lock
}

_managed_worker_install_release_lock() {
  local status=0
  if [[ "$MANAGED_WORKER_INSTALL_LOCK_HELD" != true ]]; then return 0; fi
  if [[ -z "$MANAGED_WORKER_INSTALL_LOCK_HELPER_PID" ]] ||
    ! _managed_worker_install_write_lock_release; then
    status=1
  else
    wait "$MANAGED_WORKER_INSTALL_LOCK_HELPER_PID" 2>/dev/null || status=1
  fi
  rm -f "$MANAGED_WORKER_INSTALL_LOCK_READY" "$MANAGED_WORKER_INSTALL_LOCK_RELEASE" 2>/dev/null || status=1
  MANAGED_WORKER_INSTALL_LOCK_HELD=false
  MANAGED_WORKER_INSTALL_LOCK_HELPER_PID=""
  return "$status"
}

_managed_worker_install_delete_keychain_account() {
  local account="$1" status=0
  if _managed_worker_install_security delete-generic-password \
    -s "$MANAGED_WORKER_INSTALL_KEYCHAIN_SERVICE" -a "$account" >/dev/null 2>&1; then
    return 0
  else
    status=$?
  fi
  [[ $status -eq 44 ]]
}

_managed_worker_install_write_keychain_value() {
  local account="$1"
  local value
  declare +x value
  value="$2"
  _managed_worker_install_valid_secret "$value" || return 1
  printf '%s\n%s\n' "$value" "$value" | _managed_worker_install_security add-generic-password -U \
    -s "$MANAGED_WORKER_INSTALL_KEYCHAIN_SERVICE" -a "$account" -w >/dev/null 2>&1
}

_managed_worker_install_write_journal() {
  local phase="$1"
  _managed_worker_install_validate_loaded_state || return 1
  if ! printf '%s\n' \
    'version=2' \
    "phase=$phase" \
    "kind=$MANAGED_WORKER_INSTALL_KIND" \
    "worker=$MANAGED_WORKER_INSTALL_WORKER" \
    "updater=$MANAGED_WORKER_INSTALL_UPDATER" \
    "plist=$MANAGED_WORKER_INSTALL_PLIST" \
    "plist_old=$MANAGED_WORKER_INSTALL_PLIST_OLD" \
    "plist_old_hash=$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH" \
    "plist_old_mode=$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" \
    "keychain_old=$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" \
    "service_loaded=$MANAGED_WORKER_INSTALL_SERVICE_LOADED" \
    "domain=$MANAGED_WORKER_INSTALL_DOMAIN" \
    "label=$MANAGED_WORKER_INSTALL_LABEL" \
    "updater_plist=$MANAGED_WORKER_INSTALL_UPDATER_PLIST" \
    "config=$MANAGED_WORKER_INSTALL_CONFIG" \
    "updater_plist_old=$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD" \
    "updater_plist_old_hash=$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH" \
    "updater_plist_old_mode=$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE" \
    "config_old=$MANAGED_WORKER_INSTALL_CONFIG_OLD" \
    "config_old_hash=$MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH" \
    "config_old_mode=$MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE" \
    "updater_service_loaded=$MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED" \
    "updater_label=$MANAGED_WORKER_INSTALL_UPDATER_LABEL" \
    >"$MANAGED_WORKER_INSTALL_JOURNAL_TMP"; then
    return 1
  fi
  if ! _managed_worker_install_sync || ! mv -f "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" "$MANAGED_WORKER_INSTALL_JOURNAL" ||
    ! _managed_worker_install_sync; then
    return 1
  fi
  MANAGED_WORKER_INSTALL_PHASE="$phase"
}

_managed_worker_install_validate_backup() {
  local backup="$1" old="$2" expected_hash="$3" expected_mode="$4" hash
  if [[ "$old" == 1 ]]; then
    [[ "$expected_mode" =~ ^[01234567]{3}$ ]] || return 1
    if [[ ! -f "$backup" || -L "$backup" ]] ||
      ! _managed_worker_pair_valid_hash "$expected_hash"; then
      [[ "$MANAGED_WORKER_INSTALL_PHASE" == committing ]] || return 1
    elif [[ "$MANAGED_WORKER_INSTALL_PHASE" != committing ]]; then
      hash="$(_managed_worker_install_hash "$backup")" || return 1
      [[ "$hash" == "$expected_hash" ]] || return 1
    fi
  elif [[ "$old" != 0 || "$expected_hash" != - || "$expected_mode" != - ]]; then
    return 1
  fi
}

_managed_worker_install_validate_loaded_state() {
  local worker_basename plist_basename updater_plist_basename config_basename
  local expected_worker expected_label expected_updater_label worker_parent updater_parent config_parent plist_parent updater_plist_parent
  worker_basename="${MANAGED_WORKER_INSTALL_WORKER##*/}"
  plist_basename="${MANAGED_WORKER_INSTALL_PLIST##*/}"
  updater_plist_basename="${MANAGED_WORKER_INSTALL_UPDATER_PLIST##*/}"
  config_basename="${MANAGED_WORKER_INSTALL_CONFIG##*/}"
  case "$MANAGED_WORKER_INSTALL_KIND" in
    source-agent)
      expected_worker="source-agent"
      expected_label="life.executor.kbase.source-agent"
      expected_updater_label="life.executor.kbase.source-agent.updater"
      ;;
    wcplus-agent)
      expected_worker="wcplus-agent"
      expected_label="life.executor.kbase.wcplus-agent"
      expected_updater_label="life.executor.kbase.wcplus-agent.updater"
      ;;
    chatlog-agent)
      expected_worker="chatlog-agent"
      expected_label="life.executor.kbase.chatlog-agent"
      expected_updater_label="life.executor.kbase.chatlog-agent.updater"
      ;;
    *) return 1 ;;
  esac
  if [[ "$worker_basename" != "$expected_worker" || "$MANAGED_WORKER_INSTALL_LABEL" != "$expected_label" ||
    "$plist_basename" != "$expected_label.plist" || "${MANAGED_WORKER_INSTALL_UPDATER##*/}" != source-agent-updater ||
    "$MANAGED_WORKER_INSTALL_UPDATER_LABEL" != "$expected_updater_label" ||
    "$updater_plist_basename" != "$expected_updater_label.plist" ||
    "$config_basename" != .source-agent-updater-config.json ]]; then
    return 1
  fi
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_KIND" || return 1
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_DOMAIN" || return 1
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_LABEL" || return 1
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_UPDATER_LABEL" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_WORKER" "$expected_worker" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_UPDATER" source-agent-updater || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_PLIST" "$expected_label.plist" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_UPDATER_PLIST" "$expected_updater_label.plist" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_CONFIG" .source-agent-updater-config.json || return 1
  worker_parent="${MANAGED_WORKER_INSTALL_WORKER%/*}"
  updater_parent="${MANAGED_WORKER_INSTALL_UPDATER%/*}"
  config_parent="${MANAGED_WORKER_INSTALL_CONFIG%/*}"
  plist_parent="${MANAGED_WORKER_INSTALL_PLIST%/*}"
  updater_plist_parent="${MANAGED_WORKER_INSTALL_UPDATER_PLIST%/*}"
  [[ "$worker_parent" == "$updater_parent" && "$updater_parent" == "$config_parent" ]] || return 1
  [[ "$plist_parent" == "$updater_plist_parent" ]] || return 1
  if [[ ! "$MANAGED_WORKER_INSTALL_DOMAIN" =~ ^gui/[0123456789]+$ ]] ||
    [[ "$MANAGED_WORKER_INSTALL_PLIST_OLD" != 0 && "$MANAGED_WORKER_INSTALL_PLIST_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD" != 0 && "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_CONFIG_OLD" != 0 && "$MANAGED_WORKER_INSTALL_CONFIG_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" != 0 && "$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" != 0 && "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED" != 0 && "$MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED" != 1 ]]; then
    return 1
  fi
  if [[ -L "$MANAGED_WORKER_INSTALL_WORKER" || (-e "$MANAGED_WORKER_INSTALL_WORKER" && ! -f "$MANAGED_WORKER_INSTALL_WORKER") ||
    -L "$MANAGED_WORKER_INSTALL_UPDATER" || (-e "$MANAGED_WORKER_INSTALL_UPDATER" && ! -f "$MANAGED_WORKER_INSTALL_UPDATER") ||
    -L "$MANAGED_WORKER_INSTALL_PLIST" || (-e "$MANAGED_WORKER_INSTALL_PLIST" && ! -f "$MANAGED_WORKER_INSTALL_PLIST") ||
    -L "$MANAGED_WORKER_INSTALL_UPDATER_PLIST" || (-e "$MANAGED_WORKER_INSTALL_UPDATER_PLIST" && ! -f "$MANAGED_WORKER_INSTALL_UPDATER_PLIST") ||
    -L "$MANAGED_WORKER_INSTALL_CONFIG" || (-e "$MANAGED_WORKER_INSTALL_CONFIG" && ! -f "$MANAGED_WORKER_INSTALL_CONFIG") ]]; then
    return 1
  fi
  _managed_worker_install_validate_backup "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" \
    "$MANAGED_WORKER_INSTALL_PLIST_OLD" "$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH" "$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" || return 1
  _managed_worker_install_validate_backup "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" \
    "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE" || return 1
  _managed_worker_install_validate_backup "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" \
    "$MANAGED_WORKER_INSTALL_CONFIG_OLD" "$MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH" "$MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE"
}

_managed_worker_install_read_journal() {
  local size line line_number=0 key value
  if [[ ! -f "$MANAGED_WORKER_INSTALL_JOURNAL" || -L "$MANAGED_WORKER_INSTALL_JOURNAL" ]]; then return 1; fi
  size="$(wc -c <"$MANAGED_WORKER_INSTALL_JOURNAL" 2>/dev/null || true)"
  size="${size//[[:space:]]/}"
  if [[ ! "$size" =~ ^[0123456789]+$ ]] || ((size > 4096)); then return 1; fi
  MANAGED_WORKER_INSTALL_PHASE=""
  MANAGED_WORKER_INSTALL_KIND=""
  MANAGED_WORKER_INSTALL_WORKER=""
  MANAGED_WORKER_INSTALL_UPDATER=""
  MANAGED_WORKER_INSTALL_PLIST=""
  MANAGED_WORKER_INSTALL_PLIST_OLD=""
  MANAGED_WORKER_INSTALL_PLIST_OLD_HASH=""
  MANAGED_WORKER_INSTALL_PLIST_OLD_MODE=""
  MANAGED_WORKER_INSTALL_KEYCHAIN_OLD=""
  MANAGED_WORKER_INSTALL_SERVICE_LOADED=""
  MANAGED_WORKER_INSTALL_DOMAIN=""
  MANAGED_WORKER_INSTALL_LABEL=""
  MANAGED_WORKER_INSTALL_UPDATER_PLIST=""
  MANAGED_WORKER_INSTALL_CONFIG=""
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD=""
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH=""
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE=""
  MANAGED_WORKER_INSTALL_CONFIG_OLD=""
  MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH=""
  MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE=""
  MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED=""
  MANAGED_WORKER_INSTALL_UPDATER_LABEL=""
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    key="${line%%=*}"
    value="${line#*=}"
    [[ "$key" != "$line" ]] || return 1
    case "$line_number:$key" in
      1:version) [[ "$value" == 2 ]] || return 1 ;;
      2:phase) MANAGED_WORKER_INSTALL_PHASE="$value" ;;
      3:kind) MANAGED_WORKER_INSTALL_KIND="$value" ;;
      4:worker) MANAGED_WORKER_INSTALL_WORKER="$value" ;;
      5:updater) MANAGED_WORKER_INSTALL_UPDATER="$value" ;;
      6:plist) MANAGED_WORKER_INSTALL_PLIST="$value" ;;
      7:plist_old) MANAGED_WORKER_INSTALL_PLIST_OLD="$value" ;;
      8:plist_old_hash) MANAGED_WORKER_INSTALL_PLIST_OLD_HASH="$value" ;;
      9:plist_old_mode) MANAGED_WORKER_INSTALL_PLIST_OLD_MODE="$value" ;;
      10:keychain_old) MANAGED_WORKER_INSTALL_KEYCHAIN_OLD="$value" ;;
      11:service_loaded) MANAGED_WORKER_INSTALL_SERVICE_LOADED="$value" ;;
      12:domain) MANAGED_WORKER_INSTALL_DOMAIN="$value" ;;
      13:label) MANAGED_WORKER_INSTALL_LABEL="$value" ;;
      14:updater_plist) MANAGED_WORKER_INSTALL_UPDATER_PLIST="$value" ;;
      15:config) MANAGED_WORKER_INSTALL_CONFIG="$value" ;;
      16:updater_plist_old) MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD="$value" ;;
      17:updater_plist_old_hash) MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH="$value" ;;
      18:updater_plist_old_mode) MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE="$value" ;;
      19:config_old) MANAGED_WORKER_INSTALL_CONFIG_OLD="$value" ;;
      20:config_old_hash) MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH="$value" ;;
      21:config_old_mode) MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE="$value" ;;
      22:updater_service_loaded) MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED="$value" ;;
      23:updater_label) MANAGED_WORKER_INSTALL_UPDATER_LABEL="$value" ;;
      *) return 1 ;;
    esac
  done <"$MANAGED_WORKER_INSTALL_JOURNAL"
  if [[ $line_number -ne 23 ]]; then return 1; fi
  case "$MANAGED_WORKER_INSTALL_PHASE" in
    prepared | published | keychain | config | plists | launching | committing) ;;
    *) return 1 ;;
  esac
  _managed_worker_install_validate_loaded_state
}

_managed_worker_install_restore_file() {
  local target="$1" backup="$2" old="$3" expected_hash="$4" expected_mode="$5"
  local restore_tmp="$target.restore.$$" hash
  if [[ "$old" == 0 ]]; then
    rm -f "$target" "$restore_tmp"
    return
  fi
  hash="$(_managed_worker_install_hash "$backup")" || return 1
  [[ "$hash" == "$expected_hash" ]] || return 1
  cp -p "$backup" "$restore_tmp" &&
    chmod "$expected_mode" "$restore_tmp" &&
    mv -f "$restore_tmp" "$target"
}

_managed_worker_install_restore_files() {
  local status=0
  _managed_worker_install_restore_file "$MANAGED_WORKER_INSTALL_PLIST" "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" \
    "$MANAGED_WORKER_INSTALL_PLIST_OLD" "$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH" "$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" || status=1
  _managed_worker_install_restore_file "$MANAGED_WORKER_INSTALL_UPDATER_PLIST" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" \
    "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH" \
    "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE" || status=1
  _managed_worker_install_restore_file "$MANAGED_WORKER_INSTALL_CONFIG" "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" \
    "$MANAGED_WORKER_INSTALL_CONFIG_OLD" "$MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH" "$MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE" || status=1
  return "$status"
}

_managed_worker_install_restore_keychain() {
  local status=0
  if [[ "$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" == 0 ]]; then
    _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_ACCOUNT"
    return
  fi
  if _managed_worker_install_read_keychain_value "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT"; then
    _managed_worker_install_write_keychain_value "$MANAGED_WORKER_INSTALL_KEYCHAIN_ACCOUNT" "$MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE" || status=1
  else
    status=1
  fi
  MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
  return "$status"
}

_managed_worker_install_remove_transaction_state() {
  local status=0
  if ! rm -f "$MANAGED_WORKER_INSTALL_JOURNAL"; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_sync; then status=1; fi
  if [[ $status -eq 0 ]]; then _managed_worker_install_after_journal_removal; fi
  if [[ $status -eq 0 ]] && ! rm -f \
    "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" \
    "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" \
    "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" \
    "$MANAGED_WORKER_INSTALL_JOURNAL_TMP"; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT"; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_sync; then status=1; fi
  return "$status"
}

_managed_worker_install_after_journal_removal() {
  :
}

_managed_worker_install_rollback_locked() {
  local status=0 restart_service=0 restart_updater_service=0 probe_status=0
  if [[ "$MANAGED_WORKER_INSTALL_PHASE" == launching ]]; then
    restart_service="$MANAGED_WORKER_INSTALL_SERVICE_LOADED"
    restart_updater_service="$MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED"
    if _managed_worker_install_probe_service "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_UPDATER_LABEL"; then
      _managed_worker_install_launchctl bootout "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_UPDATER_LABEL" >/dev/null 2>&1 || status=1
    else
      probe_status=$?
      [[ $probe_status -eq 1 ]] || status=1
    fi
    if _managed_worker_install_probe_service "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL"; then
      _managed_worker_install_launchctl bootout "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL" >/dev/null 2>&1 || status=1
    else
      probe_status=$?
      [[ $probe_status -eq 1 ]] || status=1
    fi
  fi
  if ! _managed_worker_install_restore_files; then status=1; fi
  if ! _managed_worker_install_restore_keychain; then status=1; fi
  if [[ "$MANAGED_WORKER_PAIR_ACTIVE" == true ]]; then
    managed_worker_pair_rollback >/dev/null 2>&1 || status=1
  else
    managed_worker_pair_recover "$MANAGED_WORKER_INSTALL_WORKER" "$MANAGED_WORKER_INSTALL_UPDATER" >/dev/null 2>&1 || status=1
  fi
  if [[ "$restart_updater_service" == 1 ]]; then
    _managed_worker_install_launchctl bootstrap "$MANAGED_WORKER_INSTALL_DOMAIN" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST" >/dev/null 2>&1 || status=1
    _managed_worker_install_launchctl print "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_UPDATER_LABEL" >/dev/null 2>&1 || status=1
  fi
  if [[ "$restart_service" == 1 ]]; then
    _managed_worker_install_launchctl bootstrap "$MANAGED_WORKER_INSTALL_DOMAIN" "$MANAGED_WORKER_INSTALL_PLIST" >/dev/null 2>&1 || status=1
    _managed_worker_install_launchctl kickstart -k "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL" >/dev/null 2>&1 || status=1
    _managed_worker_install_launchctl print "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL" >/dev/null 2>&1 || status=1
  fi
  return "$status"
}

_managed_worker_install_finish_commit_locked() {
  local status=0
  managed_worker_pair_finish_commit "$MANAGED_WORKER_INSTALL_WORKER" "$MANAGED_WORKER_INSTALL_UPDATER" >/dev/null 2>&1 || status=1
  return "$status"
}

_managed_worker_install_recover_locked() {
  if [[ ! -e "$MANAGED_WORKER_INSTALL_JOURNAL" && ! -L "$MANAGED_WORKER_INSTALL_JOURNAL" ]]; then
    if [[ -L "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" || (-e "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" && ! -f "$MANAGED_WORKER_INSTALL_JOURNAL_TMP") ||
      -L "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" || (-e "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" && ! -f "$MANAGED_WORKER_INSTALL_PLIST_BACKUP") ||
      -L "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" || (-e "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" && ! -f "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP") ||
      -L "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" || (-e "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" && ! -f "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP") ]]; then
      return 1
    fi
    if ! rm -f "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" \
      "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP"; then return 1; fi
    _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT"
    return
  fi
  _managed_worker_install_read_journal || return 1
  if [[ "$MANAGED_WORKER_INSTALL_PHASE" == committing ]]; then
    _managed_worker_install_finish_commit_locked
  else
    _managed_worker_install_rollback_locked
  fi
}

_managed_worker_install_snapshot_file() {
  local target="$1" backup="$2" slot="$3" mode hash
  if [[ ! -e "$target" ]]; then return 0; fi
  if [[ -L "$target" || ! -f "$target" ]]; then return 1; fi
  mode="$(stat -f '%Lp' "$target" 2>/dev/null || true)"
  [[ "$mode" =~ ^[01234567]{3}$ ]] || return 1
  cp -p "$target" "$backup" || return 1
  chmod 0600 "$backup" || return 1
  hash="$(_managed_worker_install_hash "$backup")" || return 1
  case "$slot" in
    plist)
      MANAGED_WORKER_INSTALL_PLIST_OLD=1
      MANAGED_WORKER_INSTALL_PLIST_OLD_HASH="$hash"
      MANAGED_WORKER_INSTALL_PLIST_OLD_MODE="$mode"
      ;;
    updater-plist)
      MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD=1
      MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH="$hash"
      MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE="$mode"
      ;;
    config)
      MANAGED_WORKER_INSTALL_CONFIG_OLD=1
      MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH="$hash"
      MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE="$mode"
      ;;
    *) return 1 ;;
  esac
}

managed_worker_install_begin() {
  local home="$1" kind="$2" worker="$3" updater="$4" plist="$5" updater_plist="$6" config="$7"
  local domain="$8" label="$9" updater_label="${10}" holder="${11}" worker_type="${12}"
  local old_status=0 status=0 recovered_transaction=false
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" == true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" == true ||
    "$MANAGED_WORKER_LIFECYCLE_LOCK_HELD" == true ]] ||
    ! _managed_worker_lifecycle_acquire "${worker%/*}" "$holder" "$worker_type"; then
    _managed_worker_lifecycle_refuse || true
    _managed_worker_install_error
    return 1
  fi
  if [[ "$MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN" != true && -f "$config" && -x "$updater" ]] &&
    ! "$updater" --check-uninstall --worker-type "$worker_type" >/dev/null 2>&1; then
    _managed_worker_lifecycle_refuse || true
    _managed_worker_install_error
    return 1
  fi
  if ! _managed_worker_install_set_home_paths "$home" || ! _managed_worker_install_prepare_root ||
    ! _managed_worker_install_acquire_lock; then
    _managed_worker_lifecycle_refuse || true
    _managed_worker_install_error
    return 1
  fi
  if [[ "$MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN" == true &&
    ! -f "$MANAGED_WORKER_INSTALL_JOURNAL" ]]; then
    _managed_worker_install_release_lock || true
    _managed_worker_lifecycle_release false || true
    _managed_worker_install_error
    return 1
  fi
  if [[ -f "$MANAGED_WORKER_INSTALL_JOURNAL" ]]; then recovered_transaction=true; fi
  if ! _managed_worker_install_recover_locked; then
    _managed_worker_install_release_lock || true
    _managed_worker_lifecycle_refuse || true
    _managed_worker_install_error
    return 1
  fi
  if [[ "$recovered_transaction" == true ]]; then
    if ! _managed_worker_lifecycle_release true || ! _managed_worker_install_remove_transaction_state; then
      _managed_worker_install_release_lock || true
      _managed_worker_install_error
      return 1
    fi
    _managed_worker_install_release_lock || true
    managed_worker_install_begin "$home" "$kind" "$worker" "$updater" "$plist" "$updater_plist" "$config" \
      "$domain" "$label" "$updater_label" "$holder" "$worker_type"
    return
  fi
  MANAGED_WORKER_INSTALL_KIND="$kind"
  MANAGED_WORKER_INSTALL_WORKER="$worker"
  MANAGED_WORKER_INSTALL_UPDATER="$updater"
  MANAGED_WORKER_INSTALL_PLIST="$plist"
  MANAGED_WORKER_INSTALL_UPDATER_PLIST="$updater_plist"
  MANAGED_WORKER_INSTALL_CONFIG="$config"
  MANAGED_WORKER_INSTALL_DOMAIN="$domain"
  MANAGED_WORKER_INSTALL_LABEL="$label"
  MANAGED_WORKER_INSTALL_UPDATER_LABEL="$updater_label"
  MANAGED_WORKER_INSTALL_PHASE=prepared
  MANAGED_WORKER_INSTALL_PLIST_OLD=0
  MANAGED_WORKER_INSTALL_PLIST_OLD_HASH=-
  MANAGED_WORKER_INSTALL_PLIST_OLD_MODE=-
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD=0
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_HASH=-
  MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD_MODE=-
  MANAGED_WORKER_INSTALL_CONFIG_OLD=0
  MANAGED_WORKER_INSTALL_CONFIG_OLD_HASH=-
  MANAGED_WORKER_INSTALL_CONFIG_OLD_MODE=-
  MANAGED_WORKER_INSTALL_KEYCHAIN_OLD=0
  MANAGED_WORKER_INSTALL_SERVICE_LOADED=0
  MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED=0
  if ! _managed_worker_install_validate_loaded_state; then status=1; fi
  if [[ $status -eq 0 ]]; then
    if _managed_worker_install_probe_service "$domain/$label"; then
      MANAGED_WORKER_INSTALL_SERVICE_LOADED=1
    else
      old_status=$?
      [[ $old_status -eq 1 ]] || status=1
    fi
  fi
  if [[ $status -eq 0 ]]; then
    if _managed_worker_install_probe_service "$domain/$updater_label"; then
      MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED=1
    else
      old_status=$?
      [[ $old_status -eq 1 ]] || status=1
    fi
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_snapshot_file "$plist" "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" plist; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_snapshot_file "$updater_plist" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" updater-plist; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_snapshot_file "$config" "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" config; then status=1; fi
  if [[ $status -eq 0 && "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" == 1 && "$MANAGED_WORKER_INSTALL_PLIST_OLD" == 0 ]]; then
    status=1
  fi
  if [[ $status -eq 0 && "$MANAGED_WORKER_INSTALL_UPDATER_SERVICE_LOADED" == 1 && "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_OLD" == 0 ]]; then
    status=1
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT"; then status=1; fi
  if [[ $status -eq 0 ]]; then
    if _managed_worker_install_read_keychain_value "$MANAGED_WORKER_INSTALL_KEYCHAIN_ACCOUNT"; then
      MANAGED_WORKER_INSTALL_KEYCHAIN_OLD=1
      _managed_worker_install_write_keychain_value "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT" "$MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE" || status=1
    else
      old_status=$?
      [[ $old_status -eq 44 ]] || status=1
    fi
  fi
  MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
  if [[ $status -eq 0 ]] && ! _managed_worker_install_write_journal prepared; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_lifecycle_message begin-mutation begun; then status=1; fi
  if [[ $status -ne 0 ]]; then
    rm -f "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" "$MANAGED_WORKER_INSTALL_UPDATER_PLIST_BACKUP" \
      "$MANAGED_WORKER_INSTALL_CONFIG_BACKUP" "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" >/dev/null 2>&1 || true
    _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT" >/dev/null 2>&1 || true
    _managed_worker_install_release_lock || true
    _managed_worker_lifecycle_release || true
    _managed_worker_install_error
    return 1
  fi
  MANAGED_WORKER_INSTALL_ACTIVE=true
}

managed_worker_install_mark() {
  local phase="$1"
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" != true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" != true ]] ||
    ! managed_worker_lifecycle_assert_alive; then return 1; fi
  case "$MANAGED_WORKER_INSTALL_PHASE:$phase" in
    prepared:published | published:keychain | keychain:config | config:plists | plists:launching | launching:committing) ;;
    *) return 1 ;;
  esac
  _managed_worker_install_write_journal "$phase"
}

# The transport credential is shared by both independent Workers. A per-Worker
# install may initialize it, but cannot silently rotate an existing value.
managed_worker_install_publish_keychain_value() {
  local requested="" status=0
  declare +x requested
  requested="$1"
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" != true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" != true ||
    "$MANAGED_WORKER_INSTALL_PHASE" != keychain ]] || ! _managed_worker_install_valid_secret "$requested"; then
    requested=""
    return 1
  fi
  if [[ "$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" == 1 ]]; then
    if ! _managed_worker_install_read_keychain_value "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT"; then
      requested=""
      MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
      return 1
    fi
    if [[ "$MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE" != "$requested" ]]; then
      requested=""
      MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
      return 65
    fi
  else
    _managed_worker_install_write_keychain_value "$MANAGED_WORKER_INSTALL_KEYCHAIN_ACCOUNT" "$requested" || status=1
  fi
  requested=""
  MANAGED_WORKER_INSTALL_KEYCHAIN_VALUE=""
  return "$status"
}

managed_worker_install_rollback() {
  local status=0
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" != true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" != true ]]; then return 0; fi
  if [[ "$MANAGED_WORKER_INSTALL_PHASE" == committing ]]; then
    _managed_worker_install_finish_commit_locked || status=1
  else
    _managed_worker_install_rollback_locked || status=1
  fi
  if [[ $status -eq 0 ]]; then
    _managed_worker_lifecycle_release true || status=1
  else
    _managed_worker_lifecycle_release false || status=1
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_remove_transaction_state; then status=1; fi
  MANAGED_WORKER_INSTALL_ACTIVE=false
  _managed_worker_install_release_lock || status=1
  if [[ $status -ne 0 ]]; then
    _managed_worker_install_rollback_error
    return 1
  fi
}

managed_worker_install_commit() {
  local status=0
  if ! managed_worker_install_mark committing; then return 1; fi
  managed_worker_pair_commit >/dev/null 2>&1 || status=1
  if [[ $status -eq 0 ]]; then
    _managed_worker_lifecycle_release true || status=1
  else
    _managed_worker_lifecycle_release false || status=1
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_remove_transaction_state; then status=1; fi
  MANAGED_WORKER_INSTALL_ACTIVE=false
  _managed_worker_install_release_lock || status=1
  if [[ $status -ne 0 ]]; then
    _managed_worker_install_error
    return 1
  fi
}
