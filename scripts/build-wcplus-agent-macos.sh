#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
pair_library="$script_dir/lib/managed-worker-pair.sh"
# shellcheck source=scripts/lib/managed-worker-pair.sh
source "$pair_library"
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
  for command_name in cp go mkdir mv rm rmdir shasum sync wc; do
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
worker_parent="$(dirname "$worker_output")"
updater_parent="$(dirname "$updater_output")"
if [[ "$worker_parent" != "$updater_parent" ]]; then
  echo "worker and updater outputs must share one directory" >&2
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
    go build -trimpath -ldflags="-s -w" -o "$worker_tmp" ./cmd/wcplus-agent
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$updater_tmp" ./cmd/source-agent-updater
)
chmod 0755 "$worker_tmp" "$updater_tmp"
if ! managed_worker_pair_publish "$worker_tmp" "$updater_tmp" "$worker_output" "$updater_output"; then
  echo "WC Plus artifact publication failed" >&2
  exit 1
fi
if ! managed_worker_pair_commit; then
  echo "WC Plus artifact commit failed" >&2
  exit 1
fi
trap - EXIT
trap - HUP INT TERM

echo "wcplus-agent built for darwin/$goarch"
echo "wcplus-agent sha256: $(shasum -a 256 "$worker_output" | awk '{print $1}')"
echo "source-agent-updater sha256: $(shasum -a 256 "$updater_output" | awk '{print $1}')"
