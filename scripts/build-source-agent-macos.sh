#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
pair_library="$script_dir/lib/managed-worker-pair.sh"
# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$pair_library"
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
for command_name in cp go mkdir mv rm rmdir shasum sync wc; do
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
  local status=$?
  if [[ "$MANAGED_WORKER_PAIR_ACTIVE" == true ]] && ! managed_worker_pair_rollback; then status=1; fi
  rm -f "$worker_tmp" "$updater_tmp" || status=1
  return "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

(
  cd "$repo_root"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$worker_tmp" ./cmd/source-agent
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$updater_tmp" ./cmd/source-agent-updater
)
chmod 0755 "$worker_tmp" "$updater_tmp"
if ! managed_worker_pair_publish "$worker_tmp" "$updater_tmp" "$worker_output" "$updater_output"; then
  echo "source-agent artifact publication failed" >&2
  exit 1
fi
if ! managed_worker_pair_commit; then
  echo "source-agent artifact commit failed" >&2
  exit 1
fi
trap - EXIT
trap - HUP INT TERM

echo "source-agent built for darwin/$goarch"
echo "source-agent sha256: $(shasum -a 256 "$worker_output" | awk '{print $1}')"
echo "source-agent-updater sha256: $(shasum -a 256 "$updater_output" | awk '{print $1}')"
