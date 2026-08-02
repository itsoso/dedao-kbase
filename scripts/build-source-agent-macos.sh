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
if [[ "$mode" == "check" ]]; then
  echo "source-agent build environment is ready for darwin/$goarch"
  exit 0
fi

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
    go build -trimpath -ldflags="-s -w" -o "$worker_tmp" ./cmd/source-agent
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$updater_tmp" ./cmd/source-agent-updater
)
chmod 0755 "$worker_tmp" "$updater_tmp"
if [[ -n "${CODESIGN_IDENTITY:-}" ]]; then
  codesign --force --options runtime --sign "$CODESIGN_IDENTITY" "$worker_tmp"
fi
mv -f "$worker_tmp" "$worker_output"
mv -f "$updater_tmp" "$updater_output"
trap - EXIT

echo "source-agent built for darwin/$goarch"
echo "source-agent sha256: $(shasum -a 256 "$worker_output" | awk '{print $1}')"
echo "source-agent-updater sha256: $(shasum -a 256 "$updater_output" | awk '{print $1}')"
