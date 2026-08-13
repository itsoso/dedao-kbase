#!/usr/bin/env bash

# Recoverable forward-only uninstall for one independent managed Worker.
# Requires managed-worker-install.sh to be sourced first.

MANAGED_WORKER_UNINSTALL_JOURNAL=""
MANAGED_WORKER_UNINSTALL_JOURNAL_TMP=""
MANAGED_WORKER_UNINSTALL_PHASE=""
MANAGED_WORKER_UNINSTALL_KIND=""
MANAGED_WORKER_UNINSTALL_WORKER=""
MANAGED_WORKER_UNINSTALL_UPDATER=""
MANAGED_WORKER_UNINSTALL_PLIST=""
MANAGED_WORKER_UNINSTALL_UPDATER_PLIST=""
MANAGED_WORKER_UNINSTALL_CONFIG=""
MANAGED_WORKER_UNINSTALL_DOMAIN=""
MANAGED_WORKER_UNINSTALL_LABEL=""
MANAGED_WORKER_UNINSTALL_UPDATER_LABEL=""
MANAGED_WORKER_UNINSTALL_WORKER_LOADED=0
MANAGED_WORKER_UNINSTALL_UPDATER_LOADED=0

_managed_worker_uninstall_error() {
  printf '%s\n' "managed worker uninstall transaction failed" >&2
}

_managed_worker_uninstall_after_phase() {
  :
}

_managed_worker_uninstall_set_journal() {
  MANAGED_WORKER_UNINSTALL_JOURNAL="$MANAGED_WORKER_INSTALL_ROOT/.managed-worker-uninstall-$MANAGED_WORKER_UNINSTALL_KIND-journal"
  MANAGED_WORKER_UNINSTALL_JOURNAL_TMP="$MANAGED_WORKER_UNINSTALL_JOURNAL.tmp"
}

_managed_worker_uninstall_validate() {
  local expected_worker expected_label expected_updater_label worker_parent updater_parent config_parent plist_parent updater_plist_parent
  case "$MANAGED_WORKER_UNINSTALL_KIND" in
    source-agent)
      expected_worker=source-agent
      expected_label=life.executor.kbase.source-agent
      expected_updater_label=life.executor.kbase.source-agent.updater
      ;;
    wcplus-agent)
      expected_worker=wcplus-agent
      expected_label=life.executor.kbase.wcplus-agent
      expected_updater_label=life.executor.kbase.wcplus-agent.updater
      ;;
    chatlog-agent)
      expected_worker=chatlog-agent
      expected_label=life.executor.kbase.chatlog-agent
      expected_updater_label=life.executor.kbase.chatlog-agent.updater
      ;;
    *) return 1 ;;
  esac
  [[ "$MANAGED_WORKER_UNINSTALL_LABEL" == "$expected_label" &&
    "$MANAGED_WORKER_UNINSTALL_UPDATER_LABEL" == "$expected_updater_label" ]] || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_UNINSTALL_WORKER" "$expected_worker" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_UNINSTALL_UPDATER" source-agent-updater || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_UNINSTALL_PLIST" "$expected_label.plist" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" "$expected_updater_label.plist" || return 1
  _managed_worker_install_canonical_target "$MANAGED_WORKER_UNINSTALL_CONFIG" .source-agent-updater-config.json || return 1
  worker_parent="${MANAGED_WORKER_UNINSTALL_WORKER%/*}"
  updater_parent="${MANAGED_WORKER_UNINSTALL_UPDATER%/*}"
  config_parent="${MANAGED_WORKER_UNINSTALL_CONFIG%/*}"
  plist_parent="${MANAGED_WORKER_UNINSTALL_PLIST%/*}"
  updater_plist_parent="${MANAGED_WORKER_UNINSTALL_UPDATER_PLIST%/*}"
  [[ "$worker_parent" == "$updater_parent" && "$updater_parent" == "$config_parent" &&
    "$plist_parent" == "$updater_plist_parent" ]] || return 1
  [[ "$MANAGED_WORKER_UNINSTALL_DOMAIN" =~ ^gui/[0123456789]+$ ]] || return 1
  [[ "$MANAGED_WORKER_UNINSTALL_WORKER_LOADED" == 0 || "$MANAGED_WORKER_UNINSTALL_WORKER_LOADED" == 1 ]] || return 1
  [[ "$MANAGED_WORKER_UNINSTALL_UPDATER_LOADED" == 0 || "$MANAGED_WORKER_UNINSTALL_UPDATER_LOADED" == 1 ]] || return 1
  local path
  for path in "$MANAGED_WORKER_UNINSTALL_WORKER" "$MANAGED_WORKER_UNINSTALL_UPDATER" \
    "$MANAGED_WORKER_UNINSTALL_PLIST" "$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" "$MANAGED_WORKER_UNINSTALL_CONFIG"; do
    if [[ -L "$path" || (-e "$path" && ! -f "$path") ]]; then return 1; fi
  done
  if [[ "$MANAGED_WORKER_UNINSTALL_PHASE" != stopped && "$MANAGED_WORKER_UNINSTALL_WORKER_LOADED" == 1 && ! -f "$MANAGED_WORKER_UNINSTALL_PLIST" ]] ||
    [[ "$MANAGED_WORKER_UNINSTALL_PHASE" != stopped && "$MANAGED_WORKER_UNINSTALL_UPDATER_LOADED" == 1 && ! -f "$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" ]]; then
    return 1
  fi
}

_managed_worker_uninstall_write_journal() {
  local phase="$1"
  _managed_worker_uninstall_validate || return 1
  printf '%s\n' \
    'version=1' "phase=$phase" "kind=$MANAGED_WORKER_UNINSTALL_KIND" \
    "worker=$MANAGED_WORKER_UNINSTALL_WORKER" "updater=$MANAGED_WORKER_UNINSTALL_UPDATER" \
    "plist=$MANAGED_WORKER_UNINSTALL_PLIST" "updater_plist=$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" \
    "config=$MANAGED_WORKER_UNINSTALL_CONFIG" "domain=$MANAGED_WORKER_UNINSTALL_DOMAIN" \
    "label=$MANAGED_WORKER_UNINSTALL_LABEL" "updater_label=$MANAGED_WORKER_UNINSTALL_UPDATER_LABEL" \
    "worker_loaded=$MANAGED_WORKER_UNINSTALL_WORKER_LOADED" \
    "updater_loaded=$MANAGED_WORKER_UNINSTALL_UPDATER_LOADED" >"$MANAGED_WORKER_UNINSTALL_JOURNAL_TMP" || return 1
  chmod 0600 "$MANAGED_WORKER_UNINSTALL_JOURNAL_TMP" || return 1
  _managed_worker_install_sync || return 1
  mv -f "$MANAGED_WORKER_UNINSTALL_JOURNAL_TMP" "$MANAGED_WORKER_UNINSTALL_JOURNAL" || return 1
  _managed_worker_install_sync || return 1
  MANAGED_WORKER_UNINSTALL_PHASE="$phase"
  _managed_worker_uninstall_after_phase "$phase"
}

_managed_worker_uninstall_read_journal() {
  local line key value line_number=0 size
  [[ -f "$MANAGED_WORKER_UNINSTALL_JOURNAL" && ! -L "$MANAGED_WORKER_UNINSTALL_JOURNAL" ]] || return 1
  size="$(wc -c <"$MANAGED_WORKER_UNINSTALL_JOURNAL" 2>/dev/null || true)"
  size="${size//[[:space:]]/}"
  [[ "$size" =~ ^[0123456789]+$ ]] && ((size <= 4096)) || return 1
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    key="${line%%=*}"
    value="${line#*=}"
    [[ "$key" != "$line" ]] || return 1
    case "$line_number:$key" in
      1:version) [[ "$value" == 1 ]] || return 1 ;;
      2:phase) MANAGED_WORKER_UNINSTALL_PHASE="$value" ;;
      3:kind) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_KIND" ]] || return 1 ;;
      4:worker) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_WORKER" ]] || return 1 ;;
      5:updater) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_UPDATER" ]] || return 1 ;;
      6:plist) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_PLIST" ]] || return 1 ;;
      7:updater_plist) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" ]] || return 1 ;;
      8:config) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_CONFIG" ]] || return 1 ;;
      9:domain) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_DOMAIN" ]] || return 1 ;;
      10:label) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_LABEL" ]] || return 1 ;;
      11:updater_label) [[ "$value" == "$MANAGED_WORKER_UNINSTALL_UPDATER_LABEL" ]] || return 1 ;;
      12:worker_loaded) MANAGED_WORKER_UNINSTALL_WORKER_LOADED="$value" ;;
      13:updater_loaded) MANAGED_WORKER_UNINSTALL_UPDATER_LOADED="$value" ;;
      *) return 1 ;;
    esac
  done <"$MANAGED_WORKER_UNINSTALL_JOURNAL"
  [[ $line_number -eq 13 ]] || return 1
  case "$MANAGED_WORKER_UNINSTALL_PHASE" in prepared | updater-stopped | stopped) ;; *) return 1 ;; esac
  _managed_worker_uninstall_validate
}

_managed_worker_uninstall_probe() {
  _managed_worker_install_probe_service "$1"
}

_managed_worker_uninstall_restore_services() {
  local status=0 probe_status=0 target
  target="$MANAGED_WORKER_UNINSTALL_DOMAIN/$MANAGED_WORKER_UNINSTALL_UPDATER_LABEL"
  if [[ "$MANAGED_WORKER_UNINSTALL_UPDATER_LOADED" == 1 ]]; then
    if _managed_worker_uninstall_probe "$target"; then
      :
    else
      probe_status=$?
      if [[ $probe_status -ne 1 ]] ||
        ! _managed_worker_install_launchctl bootstrap "$MANAGED_WORKER_UNINSTALL_DOMAIN" "$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" >/dev/null 2>&1; then status=1; fi
    fi
  fi
  target="$MANAGED_WORKER_UNINSTALL_DOMAIN/$MANAGED_WORKER_UNINSTALL_LABEL"
  if [[ "$MANAGED_WORKER_UNINSTALL_WORKER_LOADED" == 1 ]]; then
    if _managed_worker_uninstall_probe "$target"; then
      :
    else
      probe_status=$?
      if [[ $probe_status -ne 1 ]] ||
        ! _managed_worker_install_launchctl bootstrap "$MANAGED_WORKER_UNINSTALL_DOMAIN" "$MANAGED_WORKER_UNINSTALL_PLIST" >/dev/null 2>&1 ||
        ! _managed_worker_install_launchctl kickstart -k "$target" >/dev/null 2>&1; then status=1; fi
    fi
  fi
  return "$status"
}

_managed_worker_uninstall_bootout_if_loaded() {
  local target="$1" probe_status=0
  if _managed_worker_uninstall_probe "$target"; then
    _managed_worker_install_launchctl bootout "$target"
    return
  fi
  probe_status=$?
  [[ $probe_status -eq 1 ]]
}

_managed_worker_uninstall_delete_files() {
  local install_root staging handoff
  install_root="${MANAGED_WORKER_UNINSTALL_WORKER%/*}"
  staging="$install_root/.source-agent-staging"
  handoff="$install_root/.source-agent-handoff"
  rm -f "$MANAGED_WORKER_UNINSTALL_WORKER" "$MANAGED_WORKER_UNINSTALL_UPDATER" \
    "$MANAGED_WORKER_UNINSTALL_PLIST" "$MANAGED_WORKER_UNINSTALL_UPDATER_PLIST" "$MANAGED_WORKER_UNINSTALL_CONFIG" || return 1
  if [[ -d "$staging" && ! -L "$staging" ]]; then rmdir "$staging" || return 1; fi
  if [[ -d "$handoff" && ! -L "$handoff" ]]; then rmdir "$handoff" || return 1; fi
  _managed_worker_install_sync
}

managed_worker_uninstall_run() {
  local home="$1" kind="$2" worker="$3" updater="$4" plist="$5" updater_plist="$6" config="$7"
  local domain="$8" label="$9" updater_label="${10}" worker_type="${11}" status=0 probe_status=0
  local transaction_resolved=false cleanup_status=0
  if ! _managed_worker_lifecycle_acquire "${worker%/*}" "$updater" "$worker_type"; then
    _managed_worker_lifecycle_refuse || true
    _managed_worker_uninstall_error
    return 1
  fi
  if [[ "$MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN" != true ]] &&
    ! "$updater" --check-uninstall --worker-type "$worker_type" >/dev/null 2>&1; then
    _managed_worker_lifecycle_refuse || true
    _managed_worker_uninstall_error
    return 1
  fi
  if ! _managed_worker_install_set_home_paths "$home" || ! _managed_worker_install_prepare_root ||
    ! _managed_worker_install_acquire_lock; then
    _managed_worker_lifecycle_refuse || true
    _managed_worker_uninstall_error
    return 1
  fi
  MANAGED_WORKER_UNINSTALL_KIND="$kind"
  MANAGED_WORKER_UNINSTALL_WORKER="$worker"
  MANAGED_WORKER_UNINSTALL_UPDATER="$updater"
  MANAGED_WORKER_UNINSTALL_PLIST="$plist"
  MANAGED_WORKER_UNINSTALL_UPDATER_PLIST="$updater_plist"
  MANAGED_WORKER_UNINSTALL_CONFIG="$config"
  MANAGED_WORKER_UNINSTALL_DOMAIN="$domain"
  MANAGED_WORKER_UNINSTALL_LABEL="$label"
  MANAGED_WORKER_UNINSTALL_UPDATER_LABEL="$updater_label"
  _managed_worker_uninstall_set_journal
  if [[ "$MANAGED_WORKER_LIFECYCLE_MUTATION_BEGUN" == true &&
    ! -f "$MANAGED_WORKER_INSTALL_JOURNAL" && ! -f "$MANAGED_WORKER_UNINSTALL_JOURNAL" ]]; then
    status=1
  fi
  if ! _managed_worker_install_recover_locked || ! _managed_worker_uninstall_validate; then status=1; fi
  if [[ $status -eq 0 && (-e "$MANAGED_WORKER_UNINSTALL_JOURNAL" || -L "$MANAGED_WORKER_UNINSTALL_JOURNAL") ]]; then
    _managed_worker_uninstall_read_journal || status=1
  elif [[ $status -eq 0 ]]; then
    MANAGED_WORKER_UNINSTALL_PHASE=prepared
    if _managed_worker_uninstall_probe "$domain/$label"; then
      MANAGED_WORKER_UNINSTALL_WORKER_LOADED=1
    else
      probe_status=$?
      [[ $probe_status -eq 1 ]] || status=1
    fi
    if [[ $status -eq 0 ]]; then
      if _managed_worker_uninstall_probe "$domain/$updater_label"; then
        MANAGED_WORKER_UNINSTALL_UPDATER_LOADED=1
      else
        probe_status=$?
        [[ $probe_status -eq 1 ]] || status=1
      fi
    fi
    if [[ $status -eq 0 ]] && ! _managed_worker_uninstall_write_journal prepared; then status=1; fi
    if [[ $status -eq 0 ]] && ! _managed_worker_lifecycle_message begin-mutation begun; then status=1; fi
  fi
  if [[ $status -eq 0 && "$MANAGED_WORKER_UNINSTALL_PHASE" == prepared ]]; then
    if ! managed_worker_lifecycle_assert_alive ||
      ! _managed_worker_uninstall_bootout_if_loaded "$domain/$updater_label" ||
      ! _managed_worker_uninstall_write_journal updater-stopped; then status=1; fi
  fi
  if [[ $status -eq 0 && "$MANAGED_WORKER_UNINSTALL_PHASE" == updater-stopped ]]; then
    if ! managed_worker_lifecycle_assert_alive ||
      ! _managed_worker_uninstall_bootout_if_loaded "$domain/$label" ||
      ! _managed_worker_uninstall_write_journal stopped; then status=1; fi
  fi
  if [[ $status -ne 0 && "$MANAGED_WORKER_UNINSTALL_PHASE" != stopped ]]; then
    if _managed_worker_uninstall_restore_services; then transaction_resolved=true; fi
  fi
  if [[ $status -eq 0 && "$MANAGED_WORKER_UNINSTALL_PHASE" == stopped ]]; then
    managed_worker_lifecycle_assert_alive && _managed_worker_uninstall_delete_files || status=1
  fi
  if [[ $status -eq 0 || "$transaction_resolved" == true ]]; then
    _managed_worker_lifecycle_release true || cleanup_status=1
  else
    _managed_worker_lifecycle_release false || cleanup_status=1
  fi
  if [[ $cleanup_status -eq 0 && ($status -eq 0 || "$transaction_resolved" == true) ]]; then
    rm -f "$MANAGED_WORKER_UNINSTALL_JOURNAL" "$MANAGED_WORKER_UNINSTALL_JOURNAL_TMP" || cleanup_status=1
    _managed_worker_install_sync || cleanup_status=1
  fi
  _managed_worker_install_release_lock || cleanup_status=1
  if [[ $status -ne 0 || $cleanup_status -ne 0 ]]; then
    _managed_worker_uninstall_error
    return 1
  fi
}
