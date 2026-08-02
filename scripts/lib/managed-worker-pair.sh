#!/usr/bin/env bash

# Recoverable publication for a managed worker and its fixed updater. This file
# is sourced by Bash 3.2 scripts; every state transition is checked explicitly
# because callers commonly invoke these functions from conditional commands.

MANAGED_WORKER_PAIR_ACTIVE=false
MANAGED_WORKER_PAIR_LOCK_HELD=false
MANAGED_WORKER_PAIR_DIRECTORY=""
MANAGED_WORKER_PAIR_WORKER=""
MANAGED_WORKER_PAIR_UPDATER=""
MANAGED_WORKER_PAIR_JOURNAL=""
MANAGED_WORKER_PAIR_JOURNAL_TMP=""
MANAGED_WORKER_PAIR_WORKER_BACKUP=""
MANAGED_WORKER_PAIR_UPDATER_BACKUP=""
MANAGED_WORKER_PAIR_LOCK=""
MANAGED_WORKER_PAIR_PHASE=""
MANAGED_WORKER_PAIR_WORKER_OLD=0
MANAGED_WORKER_PAIR_UPDATER_OLD=0
MANAGED_WORKER_PAIR_WORKER_NEW_HASH=""
MANAGED_WORKER_PAIR_UPDATER_NEW_HASH=""
MANAGED_WORKER_PAIR_WORKER_OLD_HASH="-"
MANAGED_WORKER_PAIR_UPDATER_OLD_HASH="-"

_managed_worker_pair_error() {
  printf '%s\n' "managed worker pair transaction failed" >&2
}

_managed_worker_pair_valid_hash() {
  [[ "$1" =~ ^[0123456789abcdef]{64}$ ]]
}

_managed_worker_pair_hash() {
  local file="$1" output hash remainder
  if [[ ! -f "$file" || -L "$file" ]]; then
    return 1
  fi
  if ! output="$(shasum -a 256 "$file" 2>/dev/null)"; then
    return 1
  fi
  hash="${output%%[[:space:]]*}"
  remainder="${output#"$hash"}"
  if ! _managed_worker_pair_valid_hash "$hash" || [[ -z "$remainder" ]]; then
    return 1
  fi
  printf '%s\n' "$hash"
}

_managed_worker_pair_sync() {
  if ! sync; then
    printf '%s\n' "managed worker pair sync failed" >&2
    return 1
  fi
}

_managed_worker_pair_set_paths() {
  local worker="$1" updater="$2" worker_directory updater_directory worker_basename updater_basename
  worker_directory="${worker%/*}"
  updater_directory="${updater%/*}"
  worker_basename="${worker##*/}"
  updater_basename="${updater##*/}"
  [[ "$worker_directory" != "$worker" ]] || worker_directory="."
  [[ "$updater_directory" != "$updater" ]] || updater_directory="."
  if [[ "$worker_directory" != "$updater_directory" || ! -d "$worker_directory" ]]; then
    return 1
  fi
  case "$worker_basename" in
    source-agent | wcplus-agent) ;;
    *) return 1 ;;
  esac
  if [[ "$updater_basename" != "source-agent-updater" || "$worker" == "$updater" ]]; then
    return 1
  fi

  MANAGED_WORKER_PAIR_DIRECTORY="$worker_directory"
  MANAGED_WORKER_PAIR_WORKER="$worker"
  MANAGED_WORKER_PAIR_UPDATER="$updater"
  MANAGED_WORKER_PAIR_JOURNAL="$worker_directory/.${worker_basename}.pair-journal"
  MANAGED_WORKER_PAIR_JOURNAL_TMP="$worker_directory/.${worker_basename}.pair-journal.tmp"
  MANAGED_WORKER_PAIR_WORKER_BACKUP="$worker_directory/.${worker_basename}.pair-worker-old"
  MANAGED_WORKER_PAIR_UPDATER_BACKUP="$worker_directory/.${worker_basename}.pair-updater-old"
  MANAGED_WORKER_PAIR_LOCK="$worker_directory/.source-agent-updater.pair-lock"
}

_managed_worker_pair_validate_destination() {
  local destination
  for destination in "$MANAGED_WORKER_PAIR_WORKER" "$MANAGED_WORKER_PAIR_UPDATER"; do
    if [[ -L "$destination" || (-e "$destination" && ! -f "$destination") ]]; then
      return 1
    fi
  done
}

_managed_worker_pair_validate_sources() {
  local worker_source="$1" updater_source="$2" worker_directory updater_directory
  worker_directory="${worker_source%/*}"
  updater_directory="${updater_source%/*}"
  [[ "$worker_directory" != "$worker_source" ]] || worker_directory="."
  [[ "$updater_directory" != "$updater_source" ]] || updater_directory="."
  if [[ "$worker_directory" != "$MANAGED_WORKER_PAIR_DIRECTORY" || "$updater_directory" != "$MANAGED_WORKER_PAIR_DIRECTORY" ||
    ! -f "$worker_source" || -L "$worker_source" || ! -f "$updater_source" || -L "$updater_source" ||
    "$worker_source" == "$MANAGED_WORKER_PAIR_WORKER" || "$worker_source" == "$MANAGED_WORKER_PAIR_UPDATER" ||
    "$updater_source" == "$MANAGED_WORKER_PAIR_WORKER" || "$updater_source" == "$MANAGED_WORKER_PAIR_UPDATER" ||
    "$worker_source" == "$updater_source" ]]; then
    return 1
  fi
}

_managed_worker_pair_acquire_lock() {
  local attempt=0 owner="" owner_size=""
  while ((attempt < 3)); do
    if mkdir "$MANAGED_WORKER_PAIR_LOCK" 2>/dev/null; then
      if ! printf '%s\n' "$$" >"$MANAGED_WORKER_PAIR_LOCK/pid"; then
        rmdir "$MANAGED_WORKER_PAIR_LOCK" 2>/dev/null || true
        return 1
      fi
      MANAGED_WORKER_PAIR_LOCK_HELD=true
      return 0
    fi
    owner=""
    if [[ -f "$MANAGED_WORKER_PAIR_LOCK/pid" && ! -L "$MANAGED_WORKER_PAIR_LOCK/pid" ]]; then
      owner_size="$(wc -c <"$MANAGED_WORKER_PAIR_LOCK/pid" 2>/dev/null)" || owner_size=""
      owner_size="${owner_size//[[:space:]]/}"
      if [[ "$owner_size" =~ ^[0123456789]+$ ]] && ((owner_size <= 32)); then
        IFS= read -r owner <"$MANAGED_WORKER_PAIR_LOCK/pid" || owner=""
      fi
    fi
    if [[ "$owner" =~ ^[0123456789]+$ ]] && kill -0 "$owner" 2>/dev/null; then
      printf '%s\n' "managed worker pair transaction is busy" >&2
      return 1
    fi
    if ! rm -f "$MANAGED_WORKER_PAIR_LOCK/pid" 2>/dev/null || ! rmdir "$MANAGED_WORKER_PAIR_LOCK" 2>/dev/null; then
      return 1
    fi
    attempt=$((attempt + 1))
  done
  return 1
}

_managed_worker_pair_release_lock() {
  local status=0
  if [[ "$MANAGED_WORKER_PAIR_LOCK_HELD" != true ]]; then
    return 0
  fi
  if ! rm -f "$MANAGED_WORKER_PAIR_LOCK/pid" 2>/dev/null; then
    status=1
  fi
  if ! rmdir "$MANAGED_WORKER_PAIR_LOCK" 2>/dev/null; then
    status=1
  fi
  MANAGED_WORKER_PAIR_LOCK_HELD=false
  return "$status"
}

_managed_worker_pair_write_journal() {
  local phase="$1"
  if ! printf '%s\n' \
    'version=1' \
    "phase=$phase" \
    "worker_old=$MANAGED_WORKER_PAIR_WORKER_OLD" \
    "updater_old=$MANAGED_WORKER_PAIR_UPDATER_OLD" \
    "worker_new_hash=$MANAGED_WORKER_PAIR_WORKER_NEW_HASH" \
    "updater_new_hash=$MANAGED_WORKER_PAIR_UPDATER_NEW_HASH" \
    "worker_old_hash=$MANAGED_WORKER_PAIR_WORKER_OLD_HASH" \
    "updater_old_hash=$MANAGED_WORKER_PAIR_UPDATER_OLD_HASH" \
    >"$MANAGED_WORKER_PAIR_JOURNAL_TMP"; then
    return 1
  fi
  if ! _managed_worker_pair_sync || ! mv -f "$MANAGED_WORKER_PAIR_JOURNAL_TMP" "$MANAGED_WORKER_PAIR_JOURNAL" || ! _managed_worker_pair_sync; then
    return 1
  fi
  MANAGED_WORKER_PAIR_PHASE="$phase"
}

_managed_worker_pair_read_journal() {
  local size line line_number=0 key value
  if [[ ! -f "$MANAGED_WORKER_PAIR_JOURNAL" || -L "$MANAGED_WORKER_PAIR_JOURNAL" ]]; then
    return 1
  fi
  if ! size="$(wc -c <"$MANAGED_WORKER_PAIR_JOURNAL" 2>/dev/null)" || [[ ! "$size" =~ ^[0123456789]+$ ]] || ((size > 1024)); then
    size="${size//[[:space:]]/}"
    if [[ ! "$size" =~ ^[0123456789]+$ ]] || ((size > 1024)); then
      return 1
    fi
  fi
  MANAGED_WORKER_PAIR_PHASE=""
  MANAGED_WORKER_PAIR_WORKER_OLD=""
  MANAGED_WORKER_PAIR_UPDATER_OLD=""
  MANAGED_WORKER_PAIR_WORKER_NEW_HASH=""
  MANAGED_WORKER_PAIR_UPDATER_NEW_HASH=""
  MANAGED_WORKER_PAIR_WORKER_OLD_HASH=""
  MANAGED_WORKER_PAIR_UPDATER_OLD_HASH=""
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    key="${line%%=*}"
    value="${line#*=}"
    if [[ "$key" == "$line" ]]; then return 1; fi
    case "$line_number:$key" in
      1:version) [[ "$value" == 1 ]] || return 1 ;;
      2:phase) MANAGED_WORKER_PAIR_PHASE="$value" ;;
      3:worker_old) MANAGED_WORKER_PAIR_WORKER_OLD="$value" ;;
      4:updater_old) MANAGED_WORKER_PAIR_UPDATER_OLD="$value" ;;
      5:worker_new_hash) MANAGED_WORKER_PAIR_WORKER_NEW_HASH="$value" ;;
      6:updater_new_hash) MANAGED_WORKER_PAIR_UPDATER_NEW_HASH="$value" ;;
      7:worker_old_hash) MANAGED_WORKER_PAIR_WORKER_OLD_HASH="$value" ;;
      8:updater_old_hash) MANAGED_WORKER_PAIR_UPDATER_OLD_HASH="$value" ;;
      *) return 1 ;;
    esac
  done <"$MANAGED_WORKER_PAIR_JOURNAL"
  if [[ $line_number -ne 8 ]] ||
    [[ "$MANAGED_WORKER_PAIR_PHASE" != prepared && "$MANAGED_WORKER_PAIR_PHASE" != published && "$MANAGED_WORKER_PAIR_PHASE" != committing ]] ||
    [[ "$MANAGED_WORKER_PAIR_WORKER_OLD" != 0 && "$MANAGED_WORKER_PAIR_WORKER_OLD" != 1 ]] ||
    [[ "$MANAGED_WORKER_PAIR_UPDATER_OLD" != 0 && "$MANAGED_WORKER_PAIR_UPDATER_OLD" != 1 ]] ||
    ! _managed_worker_pair_valid_hash "$MANAGED_WORKER_PAIR_WORKER_NEW_HASH" ||
    ! _managed_worker_pair_valid_hash "$MANAGED_WORKER_PAIR_UPDATER_NEW_HASH"; then
    return 1
  fi
  if [[ "$MANAGED_WORKER_PAIR_WORKER_OLD" == 1 ]]; then
    _managed_worker_pair_valid_hash "$MANAGED_WORKER_PAIR_WORKER_OLD_HASH" || return 1
  elif [[ "$MANAGED_WORKER_PAIR_WORKER_OLD_HASH" != - ]]; then
    return 1
  fi
  if [[ "$MANAGED_WORKER_PAIR_UPDATER_OLD" == 1 ]]; then
    _managed_worker_pair_valid_hash "$MANAGED_WORKER_PAIR_UPDATER_OLD_HASH" || return 1
  elif [[ "$MANAGED_WORKER_PAIR_UPDATER_OLD_HASH" != - ]]; then
    return 1
  fi
}

_managed_worker_pair_remove_transaction_files() {
  local status=0 path
  for path in "$MANAGED_WORKER_PAIR_WORKER_BACKUP" "$MANAGED_WORKER_PAIR_UPDATER_BACKUP" "$MANAGED_WORKER_PAIR_JOURNAL_TMP" "$MANAGED_WORKER_PAIR_JOURNAL"; do
    if ! rm -f "$path"; then status=1; fi
  done
  if ! _managed_worker_pair_sync; then status=1; fi
  return "$status"
}

_managed_worker_pair_restore_side() {
  local destination="$1" backup="$2" old_present="$3" old_hash="$4" destination_hash="" backup_hash=""
  if [[ "$old_present" == 0 ]]; then
    if ! rm -f "$destination" "$backup"; then return 1; fi
    return 0
  fi
  if [[ -f "$destination" && ! -L "$destination" ]]; then
    destination_hash="$(_managed_worker_pair_hash "$destination")" || destination_hash=""
  fi
  if [[ "$destination_hash" == "$old_hash" ]]; then
    rm -f "$backup"
    return
  fi
  if [[ ! -f "$backup" || -L "$backup" ]]; then
    return 1
  fi
  backup_hash="$(_managed_worker_pair_hash "$backup")" || return 1
  if [[ "$backup_hash" != "$old_hash" ]]; then
    return 1
  fi
  mv -f "$backup" "$destination"
}

_managed_worker_pair_rollback_locked() {
  local status=0
  if [[ "$MANAGED_WORKER_PAIR_PHASE" == committing ]]; then
    return 1
  fi
  if ! _managed_worker_pair_restore_side "$MANAGED_WORKER_PAIR_WORKER" "$MANAGED_WORKER_PAIR_WORKER_BACKUP" "$MANAGED_WORKER_PAIR_WORKER_OLD" "$MANAGED_WORKER_PAIR_WORKER_OLD_HASH"; then
    status=1
  fi
  if ! _managed_worker_pair_restore_side "$MANAGED_WORKER_PAIR_UPDATER" "$MANAGED_WORKER_PAIR_UPDATER_BACKUP" "$MANAGED_WORKER_PAIR_UPDATER_OLD" "$MANAGED_WORKER_PAIR_UPDATER_OLD_HASH"; then
    status=1
  fi
  if ! _managed_worker_pair_sync; then status=1; fi
  if [[ $status -eq 0 ]]; then
    if ! _managed_worker_pair_remove_transaction_files; then status=1; fi
  fi
  return "$status"
}

_managed_worker_pair_finish_commit_locked() {
  local worker_hash updater_hash status=0
  worker_hash="$(_managed_worker_pair_hash "$MANAGED_WORKER_PAIR_WORKER")" || return 1
  updater_hash="$(_managed_worker_pair_hash "$MANAGED_WORKER_PAIR_UPDATER")" || return 1
  if [[ "$worker_hash" != "$MANAGED_WORKER_PAIR_WORKER_NEW_HASH" || "$updater_hash" != "$MANAGED_WORKER_PAIR_UPDATER_NEW_HASH" ]]; then
    return 1
  fi
  if ! rm -f "$MANAGED_WORKER_PAIR_WORKER_BACKUP"; then status=1; fi
  if ! rm -f "$MANAGED_WORKER_PAIR_UPDATER_BACKUP"; then status=1; fi
  if ! _managed_worker_pair_sync; then status=1; fi
  if [[ $status -ne 0 ]]; then return 1; fi
  if ! rm -f "$MANAGED_WORKER_PAIR_JOURNAL_TMP" "$MANAGED_WORKER_PAIR_JOURNAL"; then return 1; fi
  _managed_worker_pair_sync
}

_managed_worker_pair_recover_locked() {
  local removed=false status=0 path
  if [[ ! -e "$MANAGED_WORKER_PAIR_JOURNAL" && ! -L "$MANAGED_WORKER_PAIR_JOURNAL" ]]; then
    for path in "$MANAGED_WORKER_PAIR_WORKER_BACKUP" "$MANAGED_WORKER_PAIR_UPDATER_BACKUP" "$MANAGED_WORKER_PAIR_JOURNAL_TMP"; do
      if [[ -e "$path" || -L "$path" ]]; then
        if ! rm -f "$path"; then status=1; fi
        removed=true
      fi
    done
    if [[ "$removed" == true ]] && ! _managed_worker_pair_sync; then status=1; fi
    return "$status"
  fi
  if ! _managed_worker_pair_read_journal; then
    return 1
  fi
  case "$MANAGED_WORKER_PAIR_PHASE" in
    prepared | published) _managed_worker_pair_rollback_locked ;;
    committing) _managed_worker_pair_finish_commit_locked ;;
    *) return 1 ;;
  esac
}

managed_worker_pair_recover() {
  local worker="$1" updater="$2" status=0
  if ! _managed_worker_pair_set_paths "$worker" "$updater" || ! _managed_worker_pair_validate_destination || ! _managed_worker_pair_acquire_lock; then
    _managed_worker_pair_error
    return 1
  fi
  if ! _managed_worker_pair_recover_locked; then status=1; fi
  if ! _managed_worker_pair_release_lock; then status=1; fi
  if [[ $status -ne 0 ]]; then
    _managed_worker_pair_error
    return 1
  fi
}

managed_worker_pair_publish() {
  local worker_source="$1" updater_source="$2" worker="$3" updater="$4"
  local status=0 hash=""
  if [[ "$MANAGED_WORKER_PAIR_ACTIVE" == true || "$MANAGED_WORKER_PAIR_LOCK_HELD" == true ]]; then
    _managed_worker_pair_error
    return 1
  fi
  if ! _managed_worker_pair_set_paths "$worker" "$updater" || ! _managed_worker_pair_validate_sources "$worker_source" "$updater_source" ||
    ! _managed_worker_pair_validate_destination || ! _managed_worker_pair_acquire_lock; then
    _managed_worker_pair_error
    return 1
  fi
  if ! _managed_worker_pair_recover_locked || ! _managed_worker_pair_validate_destination; then
    _managed_worker_pair_release_lock || true
    _managed_worker_pair_error
    return 1
  fi
  if [[ $status -eq 0 ]]; then
    MANAGED_WORKER_PAIR_WORKER_NEW_HASH="$(_managed_worker_pair_hash "$worker_source")" || status=1
    MANAGED_WORKER_PAIR_UPDATER_NEW_HASH="$(_managed_worker_pair_hash "$updater_source")" || status=1
  fi
  MANAGED_WORKER_PAIR_WORKER_OLD=0
  MANAGED_WORKER_PAIR_UPDATER_OLD=0
  MANAGED_WORKER_PAIR_WORKER_OLD_HASH="-"
  MANAGED_WORKER_PAIR_UPDATER_OLD_HASH="-"
  if [[ $status -eq 0 && -e "$worker" ]]; then
    MANAGED_WORKER_PAIR_WORKER_OLD=1
    hash="$(_managed_worker_pair_hash "$worker")" || status=1
    MANAGED_WORKER_PAIR_WORKER_OLD_HASH="$hash"
    if [[ $status -eq 0 ]] && ! cp -p "$worker" "$MANAGED_WORKER_PAIR_WORKER_BACKUP"; then status=1; fi
  fi
  if [[ $status -eq 0 && -e "$updater" ]]; then
    MANAGED_WORKER_PAIR_UPDATER_OLD=1
    hash="$(_managed_worker_pair_hash "$updater")" || status=1
    MANAGED_WORKER_PAIR_UPDATER_OLD_HASH="$hash"
    if [[ $status -eq 0 ]] && ! cp -p "$updater" "$MANAGED_WORKER_PAIR_UPDATER_BACKUP"; then status=1; fi
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_pair_sync; then status=1; fi
  if [[ $status -eq 0 ]] && ! _managed_worker_pair_write_journal prepared; then status=1; fi
  if [[ $status -ne 0 ]]; then
    rm -f "$MANAGED_WORKER_PAIR_WORKER_BACKUP" "$MANAGED_WORKER_PAIR_UPDATER_BACKUP" "$MANAGED_WORKER_PAIR_JOURNAL_TMP" "$MANAGED_WORKER_PAIR_JOURNAL" 2>/dev/null || true
    _managed_worker_pair_sync >/dev/null 2>&1 || true
    _managed_worker_pair_release_lock || true
    _managed_worker_pair_error
    return 1
  fi
  MANAGED_WORKER_PAIR_ACTIVE=true

  if ! mv -f "$worker_source" "$worker" || ! _managed_worker_pair_sync || ! mv -f "$updater_source" "$updater" || ! _managed_worker_pair_sync; then
    status=1
  fi
  if [[ $status -eq 0 ]]; then
    hash="$(_managed_worker_pair_hash "$worker")" || status=1
    [[ "$hash" == "$MANAGED_WORKER_PAIR_WORKER_NEW_HASH" ]] || status=1
    hash="$(_managed_worker_pair_hash "$updater")" || status=1
    [[ "$hash" == "$MANAGED_WORKER_PAIR_UPDATER_NEW_HASH" ]] || status=1
  fi
  if [[ $status -eq 0 ]] && ! _managed_worker_pair_write_journal published; then status=1; fi
  if [[ $status -ne 0 ]]; then
    if ! _managed_worker_pair_rollback_locked; then
      printf '%s\n' "managed worker pair rollback failed" >&2
    fi
    MANAGED_WORKER_PAIR_ACTIVE=false
    _managed_worker_pair_release_lock || true
    _managed_worker_pair_error
    return 1
  fi
}

managed_worker_pair_rollback() {
  local status=0
  if [[ "$MANAGED_WORKER_PAIR_ACTIVE" != true || "$MANAGED_WORKER_PAIR_LOCK_HELD" != true ]]; then
    return 0
  fi
  if [[ "$MANAGED_WORKER_PAIR_PHASE" == committing ]]; then
    if ! _managed_worker_pair_finish_commit_locked; then status=1; fi
  else
    if ! _managed_worker_pair_rollback_locked; then status=1; fi
  fi
  MANAGED_WORKER_PAIR_ACTIVE=false
  if ! _managed_worker_pair_release_lock; then status=1; fi
  if [[ $status -ne 0 ]]; then
    printf '%s\n' "managed worker pair rollback failed" >&2
    return 1
  fi
}

managed_worker_pair_commit() {
  local worker_hash updater_hash status=0
  if [[ "$MANAGED_WORKER_PAIR_ACTIVE" != true || "$MANAGED_WORKER_PAIR_LOCK_HELD" != true ]]; then
    _managed_worker_pair_error
    return 1
  fi
  worker_hash="$(_managed_worker_pair_hash "$MANAGED_WORKER_PAIR_WORKER")" || return 1
  updater_hash="$(_managed_worker_pair_hash "$MANAGED_WORKER_PAIR_UPDATER")" || return 1
  if [[ "$worker_hash" != "$MANAGED_WORKER_PAIR_WORKER_NEW_HASH" || "$updater_hash" != "$MANAGED_WORKER_PAIR_UPDATER_NEW_HASH" ]]; then
    return 1
  fi
  if ! _managed_worker_pair_write_journal committing; then
    return 1
  fi
  if ! _managed_worker_pair_finish_commit_locked; then status=1; fi
  MANAGED_WORKER_PAIR_ACTIVE=false
  if ! _managed_worker_pair_release_lock; then status=1; fi
  if [[ $status -ne 0 ]]; then
    _managed_worker_pair_error
    return 1
  fi
}
