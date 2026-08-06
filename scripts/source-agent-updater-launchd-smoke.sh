#!/usr/bin/env bash

set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
  echo "source-agent updater launchd smoke requires macOS" >&2
  exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/source-agent-updater-launchd.XXXXXX")"
label="life.executor.kbase.test.source-agent-updater.$$.${RANDOM}"
domain="gui/$(id -u)"
target="$domain/$label"
plist="$tmp_dir/$label.plist"
fixture="$tmp_dir/updater-fixture.sh"
marker="$tmp_dir/updater.pending"
pid_file="$tmp_dir/updater.pid"
start_count="$tmp_dir/start-count"
mode_file="$tmp_dir/mode"
journal="$tmp_dir/transaction.journal"
ready="$tmp_dir/transaction.ready"
outcome="$tmp_dir/transaction.outcome"

cleanup() {
  local status=$?
  launchctl bootout "$target" >/dev/null 2>&1 || true
  rm -rf "$tmp_dir"
  return "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

cat >"$fixture" <<'FIXTURE'
#!/bin/bash
set -euo pipefail
count=0
[[ ! -f "${START_COUNT:?}" ]] || read -r count <"$START_COUNT"
count=$((count + 1))
printf '%s\n' "$count" >"$START_COUNT"
printf '%s\n' "$$" >"${PID_FILE:?}"
if [[ -f "${JOURNAL:?}" ]]; then
  printf 'recovered\n' >"${OUTCOME:?}"
else
  mode="$(<"${MODE_FILE:?}")"
  if [[ "$mode" == mid-transaction ]]; then
    printf 'replacement-started\n' >"$JOURNAL"
    printf 'ready-for-kill\n' >"${READY:?}"
  fi
fi
while [[ -e "${MARKER:?}" ]]; do /bin/sleep 0.1; done
FIXTURE
chmod 0755 "$fixture"

cat >"$plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$label</string>
  <key>ProgramArguments</key><array><string>$fixture</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>MARKER</key><string>$marker</string>
    <key>PID_FILE</key><string>$pid_file</string>
    <key>START_COUNT</key><string>$start_count</string>
    <key>MODE_FILE</key><string>$mode_file</string>
    <key>JOURNAL</key><string>$journal</string>
    <key>READY</key><string>$ready</string>
    <key>OUTCOME</key><string>$outcome</string>
  </dict>
  <key>RunAtLoad</key><false/>
  <key>KeepAlive</key><dict><key>PathState</key><dict><key>$marker</key><true/></dict></dict>
  <key>ThrottleInterval</key><integer>1</integer>
</dict>
</plist>
PLIST
plutil -lint "$plist" >/dev/null

wait_for_file() {
  local path="$1" attempt=0
  while ((attempt < 200)); do
    [[ -s "$path" ]] && return 0
    /bin/sleep 0.05
    attempt=$((attempt + 1))
  done
  return 1
}

wait_for_new_pid() {
  local previous="$1" attempt=0 candidate=""
  while ((attempt < 200)); do
    if [[ -s "$pid_file" ]]; then
      candidate="$(<"$pid_file")"
      if [[ "$candidate" =~ ^[0123456789]+$ && "$candidate" != "$previous" ]] && kill -0 "$candidate" 2>/dev/null; then
        printf '%s\n' "$candidate"
        return 0
      fi
    fi
    /bin/sleep 0.05
    attempt=$((attempt + 1))
  done
  return 1
}

printf 'before-journal\n' >"$mode_file"
: >"$marker"
launchctl bootstrap "$domain" "$plist"
wait_for_file "$pid_file"
first_pid="$(<"$pid_file")"
kill -KILL "$first_pid"
second_pid="$(wait_for_new_pid "$first_pid")"

launchctl kickstart "$target"
/bin/sleep 0.3
if [[ "$(<"$pid_file")" != "$second_pid" ]]; then
  echo "replayed updater start replaced an already running process" >&2
  exit 1
fi

printf 'mid-transaction\n' >"$mode_file"
kill -KILL "$second_pid"
third_pid="$(wait_for_new_pid "$second_pid")"
wait_for_file "$ready"
kill -KILL "$third_pid"
fourth_pid="$(wait_for_new_pid "$third_pid")"
wait_for_file "$outcome"
if [[ "$fourth_pid" == "$third_pid" || "$(<"$outcome")" != recovered ]]; then
  echo "updater did not recover a durable mid-transaction journal" >&2
  exit 1
fi

rm -f "$marker"
launchctl bootout "$target"
trap - EXIT HUP INT TERM
rm -rf "$tmp_dir"
echo "source-agent updater launchd smoke passed"
