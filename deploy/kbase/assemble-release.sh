#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSEMBLER="${SCRIPT_DIR}/assemble-release.sh"
SCHEMA="dedao-kbase-source-release/v1"
ARCHIVE_NAME="source.tar.gz"
MANIFEST_NAME="release-manifest.json"
ARCHIVE_PREFIX="dedao-kbase-source/"

usage() {
  cat <<'USAGE'
Usage:
  assemble-release.sh create --repo PATH --revision REVISION --output-dir PATH
  assemble-release.sh verify --manifest PATH [--node-bin PATH]
USAGE
}

fail() {
  printf 'assemble-release: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

require_executable() {
  value="$1"
  label="$2"
  if [[ "$value" == */* ]]; then
    [[ -x "$value" ]] || fail "${label} is not executable: $value"
  else
    command -v "$value" >/dev/null 2>&1 ||
      fail "${label} command not found: $value"
  fi
}

require_option_value() {
  option="$1"
  value="${2:-}"
  [[ -n "$value" ]] || fail "${option} requires a value"
}

create_release() {
  repo=""
  revision=""
  output_dir=""

  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --repo)
        require_option_value "$1" "${2:-}"
        repo="$2"
        shift 2
        ;;
      --revision)
        require_option_value "$1" "${2:-}"
        revision="$2"
        shift 2
        ;;
      --output-dir)
        require_option_value "$1" "${2:-}"
        output_dir="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown create option: $1"
        ;;
    esac
  done

  [[ -n "$repo" ]] || fail "create requires --repo"
  [[ -n "$revision" ]] || fail "create requires --revision"
  [[ -n "$output_dir" ]] || fail "create requires --output-dir"
  case "$revision" in
    -*) fail "revision must not begin with '-'" ;;
  esac

  require_command git
  require_command gzip
  require_command node

  [[ -d "$repo" ]] || fail "repository does not exist: $repo"
  git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
    fail "not a Git worktree: $repo"

  worktree_status="$(git -C "$repo" status --porcelain --untracked-files=all)"
  [[ -z "$worktree_status" ]] || fail "Git worktree must be clean"

  canonical_revision="$(
    git -C "$repo" rev-parse --verify "${revision}^{commit}" 2>/dev/null
  )" || fail "revision does not resolve to a commit: $revision"
  case "$canonical_revision" in
    ""|*[!0-9a-f]*) fail "Git returned a malformed canonical revision" ;;
  esac
  [[ "${#canonical_revision}" -eq 40 ]] ||
    fail "Git returned a malformed canonical revision"

  output_name="${output_dir##*/}"
  case "$output_name" in
    ""|"."|"..") fail "output directory must have a valid final name" ;;
  esac
  if [[ "$output_dir" == */* ]]; then
    output_parent="${output_dir%/*}"
    [[ -n "$output_parent" ]] || output_parent="/"
  else
    output_parent="."
  fi

  [[ -d "$output_parent" ]] ||
    fail "output parent directory does not exist: $output_parent"
  if [[ -e "$output_dir" || -L "$output_dir" ]]; then
    fail "output directory already exists: $output_dir"
  fi

  temporary_dir="$(
    mktemp -d "${output_parent}/.${output_name}.staging.XXXXXX"
  )"
  cleanup_staging() {
    if [[ -n "${temporary_dir:-}" && -d "$temporary_dir" ]]; then
      rm -rf "$temporary_dir"
    fi
  }
  trap cleanup_staging EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  temporary_archive="${temporary_dir}/${ARCHIVE_NAME}"
  temporary_manifest="${temporary_dir}/${MANIFEST_NAME}"

  git -C "$repo" archive \
    --format=tar \
    --prefix="$ARCHIVE_PREFIX" \
    "$canonical_revision" |
    gzip -n >"$temporary_archive"

  node - \
    "$temporary_archive" \
    "$temporary_manifest" \
    "$SCHEMA" \
    "$canonical_revision" \
    "$ARCHIVE_NAME" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");

const archivePath = process.argv[2];
const manifestPath = process.argv[3];
const schema = process.argv[4];
const revision = process.argv[5];
const artifact = process.argv[6];
const hash = crypto.createHash("sha256");
const stream = fs.createReadStream(archivePath);

stream.on("error", (error) => {
  process.stderr.write(`assemble-release: cannot hash archive: ${error.message}\n`);
  process.exitCode = 1;
});
stream.on("data", (chunk) => hash.update(chunk));
stream.on("end", () => {
  const manifest = {
    schema,
    revision,
    artifact,
    sha256: hash.digest("hex"),
  };
  fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o644,
  });
});
NODE

  "$ASSEMBLER" verify --manifest "$temporary_manifest"
  if [[ -e "$output_dir" || -L "$output_dir" ]]; then
    fail "output directory appeared during assembly: $output_dir"
  fi
  mv "$temporary_dir" "$output_dir"
  temporary_dir=""
  printf 'source release created: %s\n' "${output_dir}/${MANIFEST_NAME}"
}

verify_release() {
  manifest=""
  node_bin="node"

  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --manifest)
        require_option_value "$1" "${2:-}"
        manifest="$2"
        shift 2
        ;;
      --node-bin)
        require_option_value "$1" "${2:-}"
        node_bin="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown verify option: $1"
        ;;
    esac
  done

  [[ -n "$manifest" ]] || fail "verify requires --manifest"
  require_executable "$node_bin" "Node"
  [[ -f "$manifest" ]] || fail "manifest does not exist: $manifest"

  "$node_bin" - "$manifest" "$SCHEMA" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const manifestPath = path.resolve(process.argv[2]);
const expectedSchema = process.argv[3];

function fail(message) {
  process.stderr.write(`assemble-release: ${message}\n`);
  process.exit(1);
}

let manifest;
try {
  manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
} catch (error) {
  fail(`invalid manifest JSON: ${error.message}`);
}

if (
  manifest === null ||
  Array.isArray(manifest) ||
  typeof manifest !== "object"
) {
  fail("manifest must be a JSON object");
}

const expectedKeys = ["artifact", "revision", "schema", "sha256"];
const actualKeys = Object.keys(manifest).sort();
if (JSON.stringify(actualKeys) !== JSON.stringify(expectedKeys)) {
  fail(`manifest fields must be exactly: ${expectedKeys.join(", ")}`);
}
if (manifest.schema !== expectedSchema) {
  fail(`unsupported manifest schema: ${String(manifest.schema)}`);
}
if (
  typeof manifest.revision !== "string" ||
  !/^[0-9a-f]{40}$/.test(manifest.revision)
) {
  fail("manifest revision must be a canonical lowercase Git commit");
}
if (
  typeof manifest.sha256 !== "string" ||
  !/^[0-9a-f]{64}$/.test(manifest.sha256)
) {
  fail("manifest sha256 must be a lowercase SHA-256 digest");
}
if (typeof manifest.artifact !== "string" || manifest.artifact.length === 0) {
  fail("manifest artifact must be a non-empty relative path");
}
if (
  path.posix.isAbsolute(manifest.artifact) ||
  path.win32.isAbsolute(manifest.artifact) ||
  manifest.artifact.includes("\\")
) {
  fail("manifest artifact must be a portable relative path");
}

const artifactSegments = manifest.artifact.split("/");
if (
  artifactSegments.some(
    (segment) => segment === "" || segment === "." || segment === "..",
  )
) {
  fail("manifest artifact contains an invalid path segment");
}

const manifestDirectory = path.dirname(manifestPath);
const artifactPath = path.resolve(manifestDirectory, manifest.artifact);
const directoryPrefix = `${manifestDirectory}${path.sep}`;
if (!artifactPath.startsWith(directoryPrefix)) {
  fail("manifest artifact escapes its release directory");
}

let artifactStat;
try {
  artifactStat = fs.lstatSync(artifactPath);
} catch (error) {
  if (error && error.code === "ENOENT") {
    fail("manifest artifact does not exist");
  }
  fail(`cannot inspect manifest artifact: ${error.message}`);
}
if (!artifactStat.isFile() || artifactStat.isSymbolicLink()) {
  fail("manifest artifact must be a regular file");
}

const realManifestDirectory = fs.realpathSync(manifestDirectory);
const realArtifactPath = fs.realpathSync(artifactPath);
const realDirectoryPrefix = `${realManifestDirectory}${path.sep}`;
if (!realArtifactPath.startsWith(realDirectoryPrefix)) {
  fail("manifest artifact resolves outside its release directory");
}

const hash = crypto.createHash("sha256");
const stream = fs.createReadStream(realArtifactPath);
stream.on("error", (error) => {
  fail(`cannot hash manifest artifact: ${error.message}`);
});
stream.on("data", (chunk) => hash.update(chunk));
stream.on("end", () => {
  const actualDigest = hash.digest("hex");
  if (actualDigest !== manifest.sha256) {
    fail("manifest artifact digest mismatch");
  }
  process.stdout.write("source release verified\n");
});
NODE
}

main() {
  operation="${1:-}"
  if [[ -z "$operation" ]]; then
    usage >&2
    exit 2
  fi
  shift

  case "$operation" in
    create) create_release "$@" ;;
    verify) verify_release "$@" ;;
    -h|--help|help)
      usage
      ;;
    *)
      fail "unknown operation: $operation"
      ;;
  esac
}

main "$@"
