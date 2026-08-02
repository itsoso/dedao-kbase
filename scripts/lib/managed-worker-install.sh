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
MANAGED_WORKER_INSTALL_JOURNAL=""
MANAGED_WORKER_INSTALL_JOURNAL_TMP=""
MANAGED_WORKER_INSTALL_PLIST_BACKUP=""
MANAGED_WORKER_INSTALL_WORKER=""
MANAGED_WORKER_INSTALL_UPDATER=""
MANAGED_WORKER_INSTALL_PLIST=""
MANAGED_WORKER_INSTALL_DOMAIN=""
MANAGED_WORKER_INSTALL_LABEL=""
MANAGED_WORKER_INSTALL_KIND=""
MANAGED_WORKER_INSTALL_PHASE=""
MANAGED_WORKER_INSTALL_PLIST_OLD=0
MANAGED_WORKER_INSTALL_PLIST_OLD_HASH="-"
MANAGED_WORKER_INSTALL_PLIST_OLD_MODE="-"
MANAGED_WORKER_INSTALL_KEYCHAIN_OLD=0
MANAGED_WORKER_INSTALL_SERVICE_LOADED=0
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
  while ((attempt < 100)); do
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
  local account="$1" value="$2"
  _managed_worker_install_valid_secret "$value" || return 1
  printf '%s\n%s\n' "$value" "$value" | _managed_worker_install_security add-generic-password -U \
    -s "$MANAGED_WORKER_INSTALL_KEYCHAIN_SERVICE" -a "$account" -w >/dev/null 2>&1
}

_managed_worker_install_write_journal() {
  local phase="$1"
  _managed_worker_install_validate_loaded_state || return 1
  if ! printf '%s\n' \
    'version=1' \
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
    >"$MANAGED_WORKER_INSTALL_JOURNAL_TMP"; then
    return 1
  fi
  if ! _managed_worker_install_sync || ! mv -f "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" "$MANAGED_WORKER_INSTALL_JOURNAL" ||
    ! _managed_worker_install_sync; then
    return 1
  fi
  MANAGED_WORKER_INSTALL_PHASE="$phase"
}

_managed_worker_install_validate_loaded_state() {
  local worker_basename plist_basename expected_worker expected_label hash worker_parent updater_parent
  worker_basename="${MANAGED_WORKER_INSTALL_WORKER##*/}"
  plist_basename="${MANAGED_WORKER_INSTALL_PLIST##*/}"
  case "$MANAGED_WORKER_INSTALL_KIND" in
    source-agent)
      expected_worker="source-agent"
      expected_label="life.executor.kbase.source-agent"
      ;;
    wcplus-agent)
      expected_worker="wcplus-agent"
      expected_label="life.executor.kbase.wcplus-agent"
      ;;
    *) return 1 ;;
  esac
  if [[ "$worker_basename" != "$expected_worker" || "$MANAGED_WORKER_INSTALL_LABEL" != "$expected_label" ||
    "$plist_basename" != "$expected_label.plist" || "${MANAGED_WORKER_INSTALL_UPDATER##*/}" != source-agent-updater ]]; then
    return 1
  fi
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_KIND" || return 1
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_DOMAIN" || return 1
  _managed_worker_install_valid_field "$MANAGED_WORKER_INSTALL_LABEL" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_WORKER" "$expected_worker" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_UPDATER" source-agent-updater || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_INSTALL_PLIST" "$expected_label.plist" || return 1
  worker_parent="${MANAGED_WORKER_INSTALL_WORKER%/*}"
  updater_parent="${MANAGED_WORKER_INSTALL_UPDATER%/*}"
  [[ "$worker_parent" == "$updater_parent" ]] || return 1
  if [[ ! "$MANAGED_WORKER_INSTALL_DOMAIN" =~ ^gui/[0123456789]+$ ]] ||
    [[ "$MANAGED_WORKER_INSTALL_PLIST_OLD" != 0 && "$MANAGED_WORKER_INSTALL_PLIST_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" != 0 && "$MANAGED_WORKER_INSTALL_KEYCHAIN_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" != 0 && "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" != 1 ]]; then
    return 1
  fi
  if [[ -L "$MANAGED_WORKER_INSTALL_WORKER" || (-e "$MANAGED_WORKER_INSTALL_WORKER" && ! -f "$MANAGED_WORKER_INSTALL_WORKER") ||
    -L "$MANAGED_WORKER_INSTALL_UPDATER" || (-e "$MANAGED_WORKER_INSTALL_UPDATER" && ! -f "$MANAGED_WORKER_INSTALL_UPDATER") ||
    -L "$MANAGED_WORKER_INSTALL_PLIST" || (-e "$MANAGED_WORKER_INSTALL_PLIST" && ! -f "$MANAGED_WORKER_INSTALL_PLIST") ]]; then
    return 1
  fi
  if [[ "$MANAGED_WORKER_INSTALL_PLIST_OLD" == 1 ]]; then
    [[ "$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" =~ ^[01234567]{3}$ ]] || return 1
    if [[ ! -f "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" || -L "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" ]] ||
      ! _managed_worker_pair_valid_hash "$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH"; then
      [[ "$MANAGED_WORKER_INSTALL_PHASE" == committing ]] || return 1
    elif [[ "$MANAGED_WORKER_INSTALL_PHASE" != committing ]]; then
      hash="$(_managed_worker_install_hash "$MANAGED_WORKER_INSTALL_PLIST_BACKUP")" || return 1
      [[ "$hash" == "$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH" ]] || return 1
    fi
  elif [[ "$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH" != - || "$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" != - ]]; then
    return 1
  fi
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
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    key="${line%%=*}"
    value="${line#*=}"
    [[ "$key" != "$line" ]] || return 1
    case "$line_number:$key" in
      1:version) [[ "$value" == 1 ]] || return 1 ;;
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
      *) return 1 ;;
    esac
  done <"$MANAGED_WORKER_INSTALL_JOURNAL"
  if [[ $line_number -ne 13 ]]; then return 1; fi
  case "$MANAGED_WORKER_INSTALL_PHASE" in
    prepared | published | keychain | plist | launching | committing) ;;
    *) return 1 ;;
  esac
  _managed_worker_install_validate_loaded_state
}

_managed_worker_install_restore_plist() {
  local restore_tmp="$MANAGED_WORKER_INSTALL_PLIST.restore.$$" hash
  if [[ "$MANAGED_WORKER_INSTALL_PLIST_OLD" == 0 ]]; then
    rm -f "$MANAGED_WORKER_INSTALL_PLIST" "$restore_tmp"
    return
  fi
  hash="$(_managed_worker_install_hash "$MANAGED_WORKER_INSTALL_PLIST_BACKUP")" || return 1
  [[ "$hash" == "$MANAGED_WORKER_INSTALL_PLIST_OLD_HASH" ]] || return 1
  cp -p "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" "$restore_tmp" &&
    chmod "$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" "$restore_tmp" &&
    mv -f "$restore_tmp" "$MANAGED_WORKER_INSTALL_PLIST"
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
  if [[ $status -eq 0 ]] && ! rm -f "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" "$MANAGED_WORKER_INSTALL_JOURNAL_TMP"; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT"; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_sync; then status=1; fi
  return "$status"
}

_managed_worker_install_after_journal_removal() {
  :
}

_managed_worker_install_rollback_locked() {
  local status=0 restart_service=0 probe_status=0
  if [[ "$MANAGED_WORKER_INSTALL_PHASE" == launching ]]; then
    restart_service="$MANAGED_WORKER_INSTALL_SERVICE_LOADED"
    if _managed_worker_install_probe_service "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL"; then
      _managed_worker_install_launchctl bootout "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL" >/dev/null 2>&1 || status=1
    else
      probe_status=$?
      [[ $probe_status -eq 1 ]] || status=1
    fi
  fi
  if ! _managed_worker_install_restore_plist; then status=1; fi
  if ! _managed_worker_install_restore_keychain; then status=1; fi
  if [[ "$MANAGED_WORKER_PAIR_ACTIVE" == true ]]; then
    managed_worker_pair_rollback >/dev/null 2>&1 || status=1
  else
    managed_worker_pair_recover "$MANAGED_WORKER_INSTALL_WORKER" "$MANAGED_WORKER_INSTALL_UPDATER" >/dev/null 2>&1 || status=1
  fi
  if [[ "$restart_service" == 1 ]]; then
    _managed_worker_install_launchctl bootstrap "$MANAGED_WORKER_INSTALL_DOMAIN" "$MANAGED_WORKER_INSTALL_PLIST" >/dev/null 2>&1 || status=1
    _managed_worker_install_launchctl kickstart -k "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL" >/dev/null 2>&1 || status=1
    _managed_worker_install_launchctl print "$MANAGED_WORKER_INSTALL_DOMAIN/$MANAGED_WORKER_INSTALL_LABEL" >/dev/null 2>&1 || status=1
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_install_remove_transaction_state; then status=1; fi
  return "$status"
}

_managed_worker_install_finish_commit_locked() {
  local status=0
  managed_worker_pair_finish_commit "$MANAGED_WORKER_INSTALL_WORKER" "$MANAGED_WORKER_INSTALL_UPDATER" >/dev/null 2>&1 || status=1
  if [[ $status -eq 0 ]] && ! _managed_worker_install_remove_transaction_state; then status=1; fi
  return "$status"
}

_managed_worker_install_recover_locked() {
  if [[ ! -e "$MANAGED_WORKER_INSTALL_JOURNAL" && ! -L "$MANAGED_WORKER_INSTALL_JOURNAL" ]]; then
    if [[ -L "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" || (-e "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" && ! -f "$MANAGED_WORKER_INSTALL_JOURNAL_TMP") ||
      -L "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" || (-e "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" && ! -f "$MANAGED_WORKER_INSTALL_PLIST_BACKUP") ]]; then
      return 1
    fi
    if ! rm -f "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" "$MANAGED_WORKER_INSTALL_PLIST_BACKUP"; then return 1; fi
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

managed_worker_install_begin() {
  local home="$1" kind="$2" worker="$3" updater="$4" plist="$5" domain="$6" label="$7"
  local old_status=0 status=0 hash=""
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" == true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" == true ]] ||
    ! _managed_worker_install_set_home_paths "$home" || ! _managed_worker_install_prepare_root ||
    ! _managed_worker_install_acquire_lock; then
    _managed_worker_install_error
    return 1
  fi
  if ! _managed_worker_install_recover_locked; then
    _managed_worker_install_release_lock || true
    _managed_worker_install_error
    return 1
  fi
  MANAGED_WORKER_INSTALL_KIND="$kind"
  MANAGED_WORKER_INSTALL_WORKER="$worker"
  MANAGED_WORKER_INSTALL_UPDATER="$updater"
  MANAGED_WORKER_INSTALL_PLIST="$plist"
  MANAGED_WORKER_INSTALL_DOMAIN="$domain"
  MANAGED_WORKER_INSTALL_LABEL="$label"
  MANAGED_WORKER_INSTALL_PHASE=prepared
  MANAGED_WORKER_INSTALL_PLIST_OLD=0
  MANAGED_WORKER_INSTALL_PLIST_OLD_HASH=-
  MANAGED_WORKER_INSTALL_PLIST_OLD_MODE=-
  MANAGED_WORKER_INSTALL_KEYCHAIN_OLD=0
  MANAGED_WORKER_INSTALL_SERVICE_LOADED=0
  if ! _managed_worker_install_validate_loaded_state; then status=1; fi
  if [[ $status -eq 0 ]]; then
    if _managed_worker_install_probe_service "$domain/$label"; then
      MANAGED_WORKER_INSTALL_SERVICE_LOADED=1
    else
      old_status=$?
      [[ $old_status -eq 1 ]] || status=1
    fi
  fi
  if [[ $status -eq 0 && -e "$plist" ]]; then
    MANAGED_WORKER_INSTALL_PLIST_OLD=1
    MANAGED_WORKER_INSTALL_PLIST_OLD_MODE="$(stat -f '%Lp' "$plist" 2>/dev/null || true)"
    if [[ ! "$MANAGED_WORKER_INSTALL_PLIST_OLD_MODE" =~ ^[01234567]{3}$ ]] ||
      ! cp -p "$plist" "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" ||
      ! chmod 0600 "$MANAGED_WORKER_INSTALL_PLIST_BACKUP"; then
      status=1
    else
      hash="$(_managed_worker_install_hash "$MANAGED_WORKER_INSTALL_PLIST_BACKUP")" || status=1
      MANAGED_WORKER_INSTALL_PLIST_OLD_HASH="$hash"
    fi
  fi
  if [[ $status -eq 0 && "$MANAGED_WORKER_INSTALL_SERVICE_LOADED" == 1 && "$MANAGED_WORKER_INSTALL_PLIST_OLD" == 0 ]]; then
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
  if [[ $status -ne 0 ]]; then
    rm -f "$MANAGED_WORKER_INSTALL_PLIST_BACKUP" "$MANAGED_WORKER_INSTALL_JOURNAL_TMP" >/dev/null 2>&1 || true
    _managed_worker_install_delete_keychain_account "$MANAGED_WORKER_INSTALL_KEYCHAIN_BACKUP_ACCOUNT" >/dev/null 2>&1 || true
    _managed_worker_install_release_lock || true
    _managed_worker_install_error
    return 1
  fi
  MANAGED_WORKER_INSTALL_ACTIVE=true
}

managed_worker_install_mark() {
  local phase="$1"
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" != true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" != true ]]; then return 1; fi
  case "$MANAGED_WORKER_INSTALL_PHASE:$phase" in
    prepared:published | published:keychain | keychain:plist | plist:launching | launching:committing) ;;
    *) return 1 ;;
  esac
  _managed_worker_install_write_journal "$phase"
}

managed_worker_install_rollback() {
  local status=0
  if [[ "$MANAGED_WORKER_INSTALL_ACTIVE" != true || "$MANAGED_WORKER_INSTALL_LOCK_HELD" != true ]]; then return 0; fi
  if [[ "$MANAGED_WORKER_INSTALL_PHASE" == committing ]]; then
    _managed_worker_install_finish_commit_locked || status=1
  else
    _managed_worker_install_rollback_locked || status=1
  fi
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
  if [[ $status -eq 0 ]] && ! _managed_worker_install_remove_transaction_state; then status=1; fi
  MANAGED_WORKER_INSTALL_ACTIVE=false
  _managed_worker_install_release_lock || status=1
  if [[ $status -ne 0 ]]; then
    _managed_worker_install_error
    return 1
  fi
}
