#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
worker_output="${SOURCE_AGENT_BINARY_PATH:-${SOURCE_AGENT_OUTPUT:-$repo_root/build/bin/source-agent}}"
updater_output="${SOURCE_AGENT_UPDATER_BINARY_PATH:-$repo_root/build/bin/source-agent-updater}"
mode="build"

usage() {
  echo "usage: build-source-agent-macos.sh [--check]" >&2
}

case "${1:-}" in
  "") ;;
  --check) mode="check" ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "source-agent macOS packaging requires Darwin" >&2
  exit 1
fi
for command_name in go shasum; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing required command: $command_name" >&2
    exit 1
  fi
done
case "$(uname -m)" in
  arm64) goarch="arm64" ;;
  x86_64) goarch="amd64" ;;
  *)
    echo "unsupported macOS architecture" >&2
    exit 1
    ;;
esac
if [[ "$worker_output" == "$updater_output" ]]; then
  echo "worker and updater output paths must differ" >&2
  exit 2
fi
worker_parent="$(dirname "$worker_output")"
updater_parent="$(dirname "$updater_output")"
if [[ "$worker_parent" != "$updater_parent" ]]; then
  echo "worker and updater outputs must share one directory" >&2
  exit 2
fi
if [[ "$mode" == "check" ]]; then
  echo "source-agent build environment is ready for darwin/$goarch"
  exit 0
fi

mkdir -p "$worker_parent"
worker_tmp="${worker_output}.tmp.$$"
updater_tmp="${updater_output}.tmp.$$"
cleanup() {
  rm -f "$worker_tmp" "$updater_tmp"
}
trap cleanup EXIT

publish_artifact_pair() {
  local worker_source="$1" updater_source="$2" worker_destination="$3" updater_destination="$4"
  local worker_backup="${worker_destination}.backup.$$" updater_backup="${updater_destination}.backup.$$"
  local worker_backed_up=false updater_backed_up=false worker_published=false updater_published=false
  local rollback_failed=false destination

  for destination in "$worker_destination" "$updater_destination"; do
    if [[ -L "$destination" || (-e "$destination" && ! -f "$destination") ]]; then
      echo "artifact destination must be a regular file" >&2
      return 1
    fi
  done
  if [[ -e "$worker_backup" || -L "$worker_backup" || -e "$updater_backup" || -L "$updater_backup" ]]; then
    echo "artifact publication backup already exists" >&2
    return 1
  fi
  if [[ -e "$worker_destination" ]]; then
    if ! mv -f "$worker_destination" "$worker_backup"; then return 1; fi
    worker_backed_up=true
  fi
  if [[ -e "$updater_destination" ]]; then
    if ! mv -f "$updater_destination" "$updater_backup"; then
      if [[ "$worker_backed_up" == true ]] && ! mv -f "$worker_backup" "$worker_destination"; then rollback_failed=true; fi
      [[ "$rollback_failed" == false ]] || echo "artifact publication rollback failed" >&2
      return 1
    fi
    updater_backed_up=true
  fi
  if mv -f "$worker_source" "$worker_destination"; then
    worker_published=true
    if mv -f "$updater_source" "$updater_destination"; then
      updater_published=true
    fi
  fi
  if [[ "$worker_published" == true && "$updater_published" == true ]]; then
    rm -f "$worker_backup" "$updater_backup"
    return 0
  fi

  if [[ "$worker_published" == true ]] && ! rm -f "$worker_destination"; then rollback_failed=true; fi
  if [[ "$updater_published" == true ]] && ! rm -f "$updater_destination"; then rollback_failed=true; fi
  if [[ "$worker_backed_up" == true ]] && ! mv -f "$worker_backup" "$worker_destination"; then rollback_failed=true; fi
  if [[ "$updater_backed_up" == true ]] && ! mv -f "$updater_backup" "$updater_destination"; then rollback_failed=true; fi
  if [[ "$rollback_failed" == true ]]; then echo "artifact publication rollback failed" >&2; fi
  return 1
}

(
  cd "$repo_root"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$worker_tmp" ./cmd/source-agent
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$updater_tmp" ./cmd/source-agent-updater
)
chmod 0755 "$worker_tmp" "$updater_tmp"
if ! publish_artifact_pair "$worker_tmp" "$updater_tmp" "$worker_output" "$updater_output"; then
  echo "source-agent artifact publication failed" >&2
  exit 1
fi
trap - EXIT

echo "source-agent built for darwin/$goarch"
echo "source-agent sha256: $(shasum -a 256 "$worker_output" | awk '{print $1}')"
echo "source-agent-updater sha256: $(shasum -a 256 "$updater_output" | awk '{print $1}')"
