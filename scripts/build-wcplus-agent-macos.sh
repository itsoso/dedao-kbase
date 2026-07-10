#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
output_path="${WCPLUS_AGENT_BINARY_PATH:-$repo_root/build/bin/wcplus-agent}"

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
if [[ "$mode" == "check" ]]; then
  echo "wcplus-agent build environment is ready"
  exit 0
fi

case "$(uname -m)" in
  arm64) goarch="arm64" ;;
  x86_64) goarch="amd64" ;;
esac

mkdir -p "$(dirname "$output_path")"
tmp_output="${output_path}.tmp.$$"
trap 'rm -f "$tmp_output"' EXIT

(
  cd "$repo_root"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$tmp_output" ./cmd/wcplus-agent
)
chmod 0755 "$tmp_output"
mv -f "$tmp_output" "$output_path"
trap - EXIT

checksum="$(shasum -a 256 "$output_path" | awk '{print $1}')"
echo "wcplus-agent built for darwin/$goarch"
echo "sha256: $checksum"
