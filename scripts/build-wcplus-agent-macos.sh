#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
worker_output="${WCPLUS_AGENT_BINARY_PATH:-$repo_root/build/bin/wcplus-agent}"
updater_output="${WCPLUS_AGENT_UPDATER_BINARY_PATH:-$repo_root/build/bin/source-agent-updater}"

usage() {
  echo "usage: build-wcplus-agent-macos.sh [--check]" >&2
}

check_environment() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "wcplus-agent macOS packaging requires Darwin" >&2
    return 1
  fi
  local command_name
  for command_name in go shasum; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "missing required command: $command_name" >&2
      return 1
    fi
  done
  case "$(uname -m)" in
    arm64 | x86_64) ;;
    *)
      echo "unsupported macOS architecture" >&2
      return 1
      ;;
  esac
}

mode="build"
case "${1:-}" in
  "") ;;
  --check) mode="check" ;;
  *)
    usage
    exit 2
    ;;
esac

check_environment
if [[ "$worker_output" == "$updater_output" ]]; then
  echo "worker and updater output paths must differ" >&2
  exit 2
fi
if [[ "$mode" == "check" ]]; then
  case "$(uname -m)" in
    arm64) check_arch="arm64" ;;
    x86_64) check_arch="amd64" ;;
  esac
  echo "wcplus-agent build environment is ready for darwin/$check_arch"
  exit 0
fi

case "$(uname -m)" in
  arm64) goarch="arm64" ;;
  x86_64) goarch="amd64" ;;
esac

mkdir -p "$(dirname "$worker_output")" "$(dirname "$updater_output")"
worker_tmp="${worker_output}.tmp.$$"
updater_tmp="${updater_output}.tmp.$$"
cleanup() {
  rm -f "$worker_tmp" "$updater_tmp"
}
trap cleanup EXIT

(
  cd "$repo_root"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$worker_tmp" ./cmd/wcplus-agent
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$updater_tmp" ./cmd/source-agent-updater
)
chmod 0755 "$worker_tmp" "$updater_tmp"
mv -f "$worker_tmp" "$worker_output"
mv -f "$updater_tmp" "$updater_output"
trap - EXIT

echo "wcplus-agent built for darwin/$goarch"
echo "wcplus-agent sha256: $(shasum -a 256 "$worker_output" | awk '{print $1}')"
echo "source-agent-updater sha256: $(shasum -a 256 "$updater_output" | awk '{print $1}')"
