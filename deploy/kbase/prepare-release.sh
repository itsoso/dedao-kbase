#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSEMBLER="${SCRIPT_DIR}/assemble-release.sh"
PREPARER="${SCRIPT_DIR}/prepare-release.sh"
SIGNATURE_HELPER="${SCRIPT_DIR}/release-signature.sh"
ARCHIVE_LISTER="${SCRIPT_DIR}/archive-list.mjs"
STAGE_HELPER="${SCRIPT_DIR}/stage-files.mjs"
SCHEMA="dedao-kbase-prepared-release/v1"
MANIFEST_NAME="prepared-manifest.json"
SIGNATURE_NAME="MANIFEST.sig"
SOURCE_MANIFEST_NAME="release-manifest.json"
SOURCE_SIGNATURE_NAME="MANIFEST.sig"
SOURCE_ARCHIVE_NAME="source.tar.gz"
SOURCE_ROOT_NAME="dedao-kbase-source"
WEB_ROOT_NAME="frontend-web"
DEFAULT_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES=268435456
DEFAULT_SOURCE_ARCHIVE_MAX_MEMBERS=100000
DEFAULT_SOURCE_ARCHIVE_MAX_FILE_BYTES=67108864
DEFAULT_SOURCE_ARCHIVE_MAX_EXPANDED_BYTES=1073741824
DEFAULT_WEB_ARCHIVE_MAX_COMPRESSED_BYTES=33554432
DEFAULT_WEB_ARCHIVE_MAX_MEMBERS=20000
DEFAULT_WEB_ARCHIVE_MAX_FILE_BYTES=33554432
DEFAULT_WEB_ARCHIVE_MAX_EXPANDED_BYTES=268435456
DEFAULT_SERVER_BINARY_MAX_BYTES=268435456
DEFAULT_NGINX_TEMPLATE_MAX_BYTES=1048576
DEFAULT_CONFIG_RENDERER_MAX_BYTES=1048576
ARCHIVE_LIST_TIMEOUT_MS=30000
ARCHIVE_LIST_STDERR_LIMIT_BYTES=1048576
SOURCE_ARCHIVE_LIST_STDOUT_LIMIT_BYTES=16777216
WEB_ARCHIVE_LIST_STDOUT_LIMIT_BYTES=4194304
RELEASE_MANIFEST_MAX_BYTES=1048576
RELEASE_SIGNATURE_MAX_BYTES=65536
RELEASE_PUBLIC_KEY_MAX_BYTES=65536

usage() {
  cat <<'USAGE'
Usage:
  prepare-release.sh create --source-manifest PATH --source-public-key PATH --output-dir PATH [tool options]
  prepare-release.sh verify --manifest PATH --trusted-public-key PATH [--node-bin PATH] [--tar-bin PATH] [--gzip-bin PATH] [--openssl-bin PATH]

Create tool options:
  --go-bin PATH
  --npm-bin PATH
  --node-bin PATH
  --tar-bin PATH
  --gzip-bin PATH
  --nginx-bin PATH
  --uname-bin PATH
  --openssl-bin PATH
  --proxy-smoke-script PATH

Source archive quotas may be lowered with:
  KBASE_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES
  KBASE_SOURCE_ARCHIVE_MAX_MEMBERS
  KBASE_SOURCE_ARCHIVE_MAX_FILE_BYTES
  KBASE_SOURCE_ARCHIVE_MAX_EXPANDED_BYTES
USAGE
}

fail() {
  printf 'prepare-release: %s\n' "$*" >&2
  exit 1
}

require_option_value() {
  option="$1"
  value="${2:-}"
  [[ -n "$value" ]] || fail "${option} requires a value"
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

require_positive_integer() {
  value="$1"
  label="$2"
  case "$value" in
    ""|*[!0-9]*) fail "${label} must be a positive integer" ;;
  esac
  [[ "$value" != "0" ]] || fail "${label} must be greater than zero"
}

require_quota_not_above() {
  value="$1"
  maximum="$2"
  label="$3"
  "$node_bin" - "$value" "$maximum" "$label" <<'NODE'
const value = BigInt(process.argv[2]);
const maximum = BigInt(process.argv[3]);
const label = process.argv[4];
if (value > maximum) {
  process.stderr.write(
    `prepare-release: ${label} may only lower the default quota\n`,
  );
  process.exit(1);
}
NODE
}

validate_output_target() {
  output_dir="$1"
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
}

validate_trusted_file_path() {
  node_bin="$1"
  pathname="$2"
  label="$3"
  "$node_bin" - "$pathname" "$label" <<'NODE'
const fs = require("fs");
const path = require("path");

const pathname = path.resolve(process.argv[2]);
const label = process.argv[3];
const uid = typeof process.getuid === "function" ? process.getuid() : 0;
const segments = pathname.split(path.sep).filter(Boolean);
let cursor = path.parse(pathname).root;

function fail(message) {
  process.stderr.write(`prepare-release: ${message}\n`);
  process.exit(1);
}

for (let index = 0; index < segments.length; index += 1) {
  cursor = path.join(cursor, segments[index]);
  const stat = fs.lstatSync(cursor);
  const final = index === segments.length - 1;
  if (stat.isSymbolicLink()) {
    fail(`${label} path must not contain symbolic links`);
  }
  if (final ? !stat.isFile() : !stat.isDirectory()) {
    fail(`${label} path has an invalid component`);
  }
  if (stat.uid !== 0 && stat.uid !== uid) {
    fail(`${label} path has an untrusted owner`);
  }
  if ((stat.mode & 0o022) !== 0) {
    const trustedStickyDirectory =
      !final && stat.uid === 0 && (stat.mode & 0o1000) !== 0;
    if (!trustedStickyDirectory) {
      fail(`${label} path is group/other writable`);
    }
  }
}
NODE
}

stage_source_release() {
  node_bin="$1"
  source_manifest="$2"
  source_input_dir="$3"
  source_public_key="$4"
  source_archive_max_bytes="$5"

  if [[ "$source_manifest" == */* ]]; then
    source_release_dir="${source_manifest%/*}"
    [[ -n "$source_release_dir" ]] || source_release_dir="/"
  else
    source_release_dir="."
  fi
  source_signature="${source_release_dir}/${SOURCE_SIGNATURE_NAME}"
  source_archive="${source_release_dir}/${SOURCE_ARCHIVE_NAME}"

  [[ -f "$source_manifest" && ! -L "$source_manifest" ]] ||
    fail "source manifest must be a regular file"
  [[ -f "$source_signature" && ! -L "$source_signature" ]] ||
    fail "source signature must be a regular file"
  [[ -f "$source_archive" && ! -L "$source_archive" ]] ||
    fail "source archive must be a regular file"

  mkdir "$source_input_dir"
  chmod 0700 "$source_input_dir"
  "$node_bin" "$STAGE_HELPER" \
    --file \
    "$source_manifest" \
    "${source_input_dir}/${SOURCE_MANIFEST_NAME}" \
    "$RELEASE_MANIFEST_MAX_BYTES" \
    0600 \
    --file \
    "$source_signature" \
    "${source_input_dir}/${SOURCE_SIGNATURE_NAME}" \
    "$RELEASE_SIGNATURE_MAX_BYTES" \
    0600 \
    --file \
    "$source_archive" \
    "${source_input_dir}/${SOURCE_ARCHIVE_NAME}" \
    "$source_archive_max_bytes" \
    0600 \
    --file \
    "$source_public_key" \
    "${source_input_dir}/source-public.pem" \
    "$RELEASE_PUBLIC_KEY_MAX_BYTES" \
    0600
}

source_manifest_value() {
  node_bin="$1"
  manifest="$2"
  field="$3"
  "$node_bin" - "$manifest" "$field" <<'NODE'
const fs = require("fs");
const path = require("path");

const manifestPath = path.resolve(process.argv[2]);
const field = process.argv[3];
const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));

if (field === "revision") {
  process.stdout.write(manifest.revision);
} else if (field === "artifact") {
  process.stdout.write(path.resolve(path.dirname(manifestPath), manifest.artifact));
} else if (field === "artifact-name") {
  process.stdout.write(manifest.artifact);
} else {
  throw new Error(`unsupported source manifest field: ${field}`);
}
NODE
}

validate_source_archive_compressed_size() {
  node_bin="$1"
  source_archive="$2"
  max_compressed_bytes="$3"
  "$node_bin" - "$source_archive" "$max_compressed_bytes" <<'NODE'
const fs = require("fs");

const archivePath = process.argv[2];
const maxCompressedBytes = BigInt(process.argv[3]);
const compressedBytes = BigInt(fs.statSync(archivePath).size);
if (compressedBytes > maxCompressedBytes) {
  process.stderr.write(
    `prepare-release: source archive compressed size exceeds quota: ${compressedBytes} > ${maxCompressedBytes}\n`,
  );
  process.exit(1);
}
NODE
}

validate_source_archive() {
  node_bin="$1"
  tar_bin="$2"
  gzip_bin="$3"
  source_archive="$4"
  validation_dir="$5"
  max_compressed_bytes="$6"
  max_members="$7"
  max_file_bytes="$8"
  max_expanded_bytes="$9"
  members_file="${validation_dir}/source-members.json"

  "$node_bin" "$ARCHIVE_LISTER" \
    --archive "$source_archive" \
    --gzip-bin "$gzip_bin" \
    --tar-bin "$tar_bin" \
    --timeout-ms "$ARCHIVE_LIST_TIMEOUT_MS" \
    --stdout-limit-bytes "$SOURCE_ARCHIVE_LIST_STDOUT_LIMIT_BYTES" \
    --stderr-limit-bytes "$ARCHIVE_LIST_STDERR_LIMIT_BYTES" \
    >"$members_file"

  "$node_bin" - \
    "$members_file" \
    "$SOURCE_ROOT_NAME" \
    "$source_archive" \
    "$max_compressed_bytes" \
    "$max_members" \
    "$max_file_bytes" \
    "$max_expanded_bytes" <<'NODE'
const fs = require("fs");

const membersPath = process.argv[2];
const expectedRoot = process.argv[3];
const archivePath = process.argv[4];
const maxCompressedBytes = BigInt(process.argv[5]);
const maxMembers = BigInt(process.argv[6]);
const maxFileBytes = BigInt(process.argv[7]);
const maxExpandedBytes = BigInt(process.argv[8]);

function fail(message) {
  process.stderr.write(`prepare-release: ${message}\n`);
  process.exit(1);
}

let document;
try {
  document = JSON.parse(fs.readFileSync(membersPath, "utf8"));
} catch (error) {
  fail(`source archive listing is invalid JSON: ${error.message}`);
}
if (
  document === null ||
  Array.isArray(document) ||
  typeof document !== "object" ||
  JSON.stringify(Object.keys(document)) !== JSON.stringify(["members"]) ||
  !Array.isArray(document.members) ||
  document.members.length === 0
) {
  fail("source archive member listing is empty or invalid");
}
const compressedBytes = BigInt(fs.statSync(archivePath).size);
if (compressedBytes > maxCompressedBytes) {
  fail(
    `source archive compressed size exceeds quota: ${compressedBytes} > ${maxCompressedBytes}`,
  );
}
if (BigInt(document.members.length) > maxMembers) {
  fail(
    `source archive member count exceeds quota: ${document.members.length} > ${maxMembers}`,
  );
}

let expandedBytes = 0n;
for (const member of document.members) {
  if (
    member === null ||
    Array.isArray(member) ||
    typeof member !== "object" ||
    JSON.stringify(Object.keys(member).sort()) !==
      JSON.stringify(["path", "size", "type"])
  ) {
    fail("source archive member metadata is invalid");
  }
  const name = member.path;
  if (
    name.length === 0 ||
    name.startsWith("/") ||
    name.includes("\\") ||
    [...name].some((character) => {
      const code = character.codePointAt(0);
      return code < 0x20 || code === 0x7f;
    })
  ) {
    fail(`source archive contains an unsafe member path: ${name}`);
  }
  if (!name.startsWith(`${expectedRoot}/`)) {
    fail(`source archive member is outside ${expectedRoot}/: ${name}`);
  }
  const segments = name.split("/");
  if (segments[segments.length - 1] === "") {
    segments.pop();
  }
  if (
    segments.length === 0 ||
    segments[0] !== expectedRoot ||
    segments.some(
      (segment) => segment === "" || segment === "." || segment === "..",
    )
  ) {
    fail(`source archive contains an invalid path segment: ${name}`);
  }

  if (member.type !== "file" && member.type !== "directory") {
    fail(`source archive member type is not allowed: ${name}`);
  }
  if (!Number.isSafeInteger(member.size) || member.size < 0) {
    fail(`source archive member has an invalid size: ${name}`);
  }
  const size = BigInt(member.size);
  if (size > maxFileBytes) {
    fail(
      `source archive member exceeds per-file quota: ${name} (${size} > ${maxFileBytes})`,
    );
  }
  expandedBytes += size;
  if (expandedBytes > maxExpandedBytes) {
    fail(
      `source archive expanded size exceeds quota: ${expandedBytes} > ${maxExpandedBytes}`,
    );
  }
}
NODE
}

validate_prepared_web_archive() {
  node_bin="$1"
  tar_bin="$2"
  gzip_bin="$3"
  archive="$4"
  validation_dir="$5"
  members_file="${validation_dir}/web-members.json"

  "$node_bin" - "$archive" "$DEFAULT_WEB_ARCHIVE_MAX_COMPRESSED_BYTES" <<'NODE'
const fs = require("fs");
const compressed = BigInt(fs.statSync(process.argv[2]).size);
const maximum = BigInt(process.argv[3]);
if (compressed > maximum) {
  process.stderr.write(
    `prepare-release: web archive compressed size exceeds quota: ${compressed} > ${maximum}\n`,
  );
  process.exit(1);
}
NODE

  "$node_bin" "$ARCHIVE_LISTER" \
    --archive "$archive" \
    --gzip-bin "$gzip_bin" \
    --tar-bin "$tar_bin" \
    --timeout-ms "$ARCHIVE_LIST_TIMEOUT_MS" \
    --stdout-limit-bytes "$WEB_ARCHIVE_LIST_STDOUT_LIMIT_BYTES" \
    --stderr-limit-bytes "$ARCHIVE_LIST_STDERR_LIMIT_BYTES" \
    >"$members_file"

  "$node_bin" - \
    "$members_file" \
    "$WEB_ROOT_NAME" \
    "$DEFAULT_WEB_ARCHIVE_MAX_MEMBERS" \
    "$DEFAULT_WEB_ARCHIVE_MAX_FILE_BYTES" \
    "$DEFAULT_WEB_ARCHIVE_MAX_EXPANDED_BYTES" <<'NODE'
const fs = require("fs");

const document = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const expectedRoot = process.argv[3];
const maxMembers = BigInt(process.argv[4]);
const maxFileBytes = BigInt(process.argv[5]);
const maxExpandedBytes = BigInt(process.argv[6]);

function fail(message) {
  process.stderr.write(`prepare-release: ${message}\n`);
  process.exit(1);
}

if (
  document === null ||
  Array.isArray(document) ||
  typeof document !== "object" ||
  JSON.stringify(Object.keys(document)) !== JSON.stringify(["members"]) ||
  !Array.isArray(document.members) ||
  document.members.length === 0
) {
  fail("web archive member listing is empty or invalid");
}
if (BigInt(document.members.length) > maxMembers) {
  fail("web archive exceeds the member count limit");
}

let expandedBytes = 0n;
for (const member of document.members) {
  if (
    member === null ||
    Array.isArray(member) ||
    typeof member !== "object" ||
    JSON.stringify(Object.keys(member).sort()) !==
      JSON.stringify(["path", "size", "type"])
  ) {
    fail("web archive member metadata is invalid");
  }
  const name = member.path;
  if (
    typeof name !== "string" ||
    name.length === 0 ||
    name.startsWith("/") ||
    name.includes("\\") ||
    [...name].some((character) => {
      const code = character.codePointAt(0);
      return code < 0x20 || code === 0x7f;
    })
  ) {
    fail(`web archive contains an unsafe member path: ${String(name)}`);
  }
  if (!name.startsWith(`${expectedRoot}/`)) {
    fail(`web archive member is outside ${expectedRoot}/: ${name}`);
  }
  const segments = name.split("/");
  if (segments[segments.length - 1] === "") {
    segments.pop();
  }
  if (
    segments.length === 0 ||
    segments[0] !== expectedRoot ||
    segments.some(
      (segment) => segment === "" || segment === "." || segment === "..",
    )
  ) {
    fail(`web archive contains an invalid path segment: ${name}`);
  }
  if (member.type !== "file" && member.type !== "directory") {
    fail(`web archive member type is not allowed: ${name}`);
  }
  if (!Number.isSafeInteger(member.size) || member.size < 0) {
    fail(`web archive member has an invalid size: ${name}`);
  }
  const size = BigInt(member.size);
  if (member.type === "file" && size > maxFileBytes) {
    fail(`web archive member exceeds the file size limit: ${name}`);
  }
  expandedBytes += size;
  if (expandedBytes > maxExpandedBytes) {
    fail("web archive exceeds the expanded size limit");
  }
}
NODE
}

verify_extracted_source() {
  node_bin="$1"
  extract_dir="$2"
  source_root="$3"

  unsafe_entry="$(
    find "$extract_dir" \
      -mindepth 1 \
      ! -type f \
      ! -type d \
      -print \
      -quit
  )"
  [[ -z "$unsafe_entry" ]] ||
    fail "extracted source contains a non-file/non-directory entry"

  "$node_bin" - "$extract_dir" "$source_root" "$SOURCE_ROOT_NAME" <<'NODE'
const fs = require("fs");
const path = require("path");

const extractDirectory = fs.realpathSync(process.argv[2]);
const sourceRoot = fs.realpathSync(process.argv[3]);
const expectedRoot = process.argv[4];
const expectedPath = path.join(extractDirectory, expectedRoot);

if (sourceRoot !== expectedPath) {
  process.stderr.write(
    "prepare-release: extracted source root resolves outside staging\n",
  );
  process.exit(1);
}
NODE
}

write_prepared_manifest() {
  node_bin="$1"
  manifest="$2"
  revision="$3"
  bundle_dir="$4"
  "$node_bin" - \
    "$manifest" \
    "$revision" \
    "$bundle_dir" \
    "$SCHEMA" \
    "$DEFAULT_SERVER_BINARY_MAX_BYTES" \
    "$DEFAULT_WEB_ARCHIVE_MAX_COMPRESSED_BYTES" \
    "$DEFAULT_NGINX_TEMPLATE_MAX_BYTES" \
    "$DEFAULT_CONFIG_RENDERER_MAX_BYTES" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const manifestPath = process.argv[2];
const revision = process.argv[3];
const bundleDirectory = process.argv[4];
const schema = process.argv[5];
const serverMaximum = Number(process.argv[6]);
const webMaximum = Number(process.argv[7]);
const nginxMaximum = Number(process.argv[8]);
const rendererMaximum = Number(process.argv[9]);
const specifications = [
  {
    name: "kbase-server",
    path: "bundle/kbase-server",
    mode: "0755",
    maximum: serverMaximum,
  },
  {
    name: "web",
    path: "bundle/web.tar.gz",
    mode: "0644",
    maximum: webMaximum,
  },
  {
    name: "nginx-template",
    path: "bundle/kbase.locations.conf.template",
    mode: "0644",
    maximum: nginxMaximum,
  },
  {
    name: "config-renderer",
    path: "bundle/render-kbase-config.sh",
    mode: "0755",
    maximum: rendererMaximum,
  },
];

function digest(filePath) {
  const hash = crypto.createHash("sha256");
  const descriptor = fs.openSync(filePath, "r");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    while (true) {
      const bytesRead = fs.readSync(
        descriptor,
        buffer,
        0,
        buffer.length,
        null,
      );
      if (bytesRead === 0) {
        return hash.digest("hex");
      }
      hash.update(buffer.subarray(0, bytesRead));
    }
  } finally {
    fs.closeSync(descriptor);
  }
}

const artifacts = specifications.map((specification) => {
  const artifactPath = path.join(
    bundleDirectory,
    path.basename(specification.path),
  );
  const stat = fs.lstatSync(artifactPath);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`prepared artifact is not a regular file: ${specification.name}`);
  }
  if (stat.size > specification.maximum) {
    throw new Error(
      `prepared artifact exceeds byte limit: ${specification.name}`,
    );
  }
  return {
    name: specification.name,
    path: specification.path,
    sha256: digest(artifactPath),
    mode: specification.mode,
  };
});
const manifest = { schema, revision, artifacts };
fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, {
  encoding: "utf8",
  mode: 0o644,
});
NODE
}

create_release() {
  source_manifest=""
  source_public_key=""
  output_dir=""
  go_bin="go"
  npm_bin="npm"
  node_bin="node"
  tar_bin="tar"
  gzip_bin="gzip"
  nginx_bin="nginx"
  uname_bin="uname"
  openssl_bin="openssl"
  proxy_smoke_script=""

  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --source-manifest)
        require_option_value "$1" "${2:-}"
        source_manifest="$2"
        shift 2
        ;;
      --source-public-key)
        require_option_value "$1" "${2:-}"
        source_public_key="$2"
        shift 2
        ;;
      --output-dir)
        require_option_value "$1" "${2:-}"
        output_dir="$2"
        shift 2
        ;;
      --go-bin)
        require_option_value "$1" "${2:-}"
        go_bin="$2"
        shift 2
        ;;
      --npm-bin)
        require_option_value "$1" "${2:-}"
        npm_bin="$2"
        shift 2
        ;;
      --node-bin)
        require_option_value "$1" "${2:-}"
        node_bin="$2"
        shift 2
        ;;
      --tar-bin)
        require_option_value "$1" "${2:-}"
        tar_bin="$2"
        shift 2
        ;;
      --gzip-bin)
        require_option_value "$1" "${2:-}"
        gzip_bin="$2"
        shift 2
        ;;
      --nginx-bin)
        require_option_value "$1" "${2:-}"
        nginx_bin="$2"
        shift 2
        ;;
      --uname-bin)
        require_option_value "$1" "${2:-}"
        uname_bin="$2"
        shift 2
        ;;
      --openssl-bin)
        require_option_value "$1" "${2:-}"
        openssl_bin="$2"
        shift 2
        ;;
      --proxy-smoke-script)
        require_option_value "$1" "${2:-}"
        proxy_smoke_script="$2"
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

  [[ -n "$source_manifest" ]] || fail "create requires --source-manifest"
  [[ -n "$source_public_key" ]] ||
    fail "create requires --source-public-key"
  [[ -n "$output_dir" ]] || fail "create requires --output-dir"
  effective_uid="${EUID:-$(id -u)}"
  [[ "$effective_uid" != "0" ]] ||
    fail "release preparation must not run as root"
  validate_output_target "$output_dir"

  [[ -x "$ASSEMBLER" ]] || fail "source release assembler is not executable"
  [[ -x "$SIGNATURE_HELPER" ]] ||
    fail "release signature helper is not executable"
  [[ -f "$ARCHIVE_LISTER" && ! -L "$ARCHIVE_LISTER" ]] ||
    fail "archive listing helper is missing"
  [[ -f "$STAGE_HELPER" && ! -L "$STAGE_HELPER" ]] ||
    fail "bounded staging helper is missing"
  require_executable "$node_bin" "Node"
  require_executable "$openssl_bin" "OpenSSL"
  [[ -f "$source_public_key" && ! -L "$source_public_key" ]] ||
    fail "source public key must be a regular file"
  validate_trusted_file_path \
    "$node_bin" \
    "$source_public_key" \
    "source public key"
  max_compressed_bytes="$(
    printf '%s' "${KBASE_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES:-$DEFAULT_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES}"
  )"
  max_members="$(
    printf '%s' "${KBASE_SOURCE_ARCHIVE_MAX_MEMBERS:-$DEFAULT_SOURCE_ARCHIVE_MAX_MEMBERS}"
  )"
  max_file_bytes="$(
    printf '%s' "${KBASE_SOURCE_ARCHIVE_MAX_FILE_BYTES:-$DEFAULT_SOURCE_ARCHIVE_MAX_FILE_BYTES}"
  )"
  max_expanded_bytes="$(
    printf '%s' "${KBASE_SOURCE_ARCHIVE_MAX_EXPANDED_BYTES:-$DEFAULT_SOURCE_ARCHIVE_MAX_EXPANDED_BYTES}"
  )"
  require_positive_integer \
    "$max_compressed_bytes" \
    "source compressed-size quota"
  require_positive_integer "$max_members" "source member-count quota"
  require_positive_integer "$max_file_bytes" "source per-file quota"
  require_positive_integer \
    "$max_expanded_bytes" \
    "source expanded-size quota"
  require_quota_not_above \
    "$max_compressed_bytes" \
    "$DEFAULT_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES" \
    "source compressed-size quota"
  require_quota_not_above \
    "$max_members" \
    "$DEFAULT_SOURCE_ARCHIVE_MAX_MEMBERS" \
    "source member-count quota"
  require_quota_not_above \
    "$max_file_bytes" \
    "$DEFAULT_SOURCE_ARCHIVE_MAX_FILE_BYTES" \
    "source per-file quota"
  require_quota_not_above \
    "$max_expanded_bytes" \
    "$DEFAULT_SOURCE_ARCHIVE_MAX_EXPANDED_BYTES" \
    "source expanded-size quota"

  temporary_dir="$(
    mktemp -d "${output_parent}/.${output_name}.staging.XXXXXX"
  )"
  chmod 0700 "$temporary_dir"
  cleanup_staging() {
    if [[ -n "${temporary_dir:-}" && -d "$temporary_dir" ]]; then
      rm -rf "$temporary_dir"
    fi
  }
  trap cleanup_staging EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  source_input_dir="${temporary_dir}/source-input"
  stage_source_release \
    "$node_bin" \
    "$source_manifest" \
    "$source_input_dir" \
    "$source_public_key" \
    "$max_compressed_bytes"
  staged_source_manifest="${source_input_dir}/${SOURCE_MANIFEST_NAME}"
  staged_source_public_key="${source_input_dir}/source-public.pem"

  # Verify and consume only the private staged source bytes.
  "$ASSEMBLER" verify \
    --node-bin "$node_bin" \
    --manifest "$staged_source_manifest" \
    --trusted-public-key "$staged_source_public_key" \
    --openssl-bin "$openssl_bin"
  revision="$(
    source_manifest_value "$node_bin" "$staged_source_manifest" revision
  )"
  source_artifact="$(
    source_manifest_value \
      "$node_bin" \
      "$staged_source_manifest" \
      artifact-name
  )"
  [[ "$source_artifact" == "$SOURCE_ARCHIVE_NAME" ]] ||
    fail "source manifest artifact must be ${SOURCE_ARCHIVE_NAME}"
  source_archive="${source_input_dir}/${SOURCE_ARCHIVE_NAME}"
  validate_source_archive_compressed_size \
    "$node_bin" \
    "$source_archive" \
    "$max_compressed_bytes"

  require_executable "$uname_bin" "uname"
  platform="$("$uname_bin" -s)"
  [[ "$platform" == "Linux" ]] ||
    fail "release preparation only supports Linux"
  export LC_ALL=C

  require_executable "$go_bin" "Go"
  require_executable "$npm_bin" "npm"
  require_executable "$tar_bin" "tar"
  require_executable "$gzip_bin" "gzip"
  require_executable "$nginx_bin" "Nginx"
  tar_version="$("$tar_bin" --version 2>&1)" ||
    fail "cannot inspect tar implementation"
  case "$tar_version" in
    *"GNU tar"*) ;;
    *) fail "release preparation requires GNU tar" ;;
  esac

  extract_dir="${temporary_dir}/source"
  bundle_dir="${temporary_dir}/bundle"
  validation_dir="${temporary_dir}/validation"
  mkdir -p "$extract_dir" "$bundle_dir" "$validation_dir"
  validate_source_archive \
    "$node_bin" \
    "$tar_bin" \
    "$gzip_bin" \
    "$source_archive" \
    "$validation_dir" \
    "$max_compressed_bytes" \
    "$max_members" \
    "$max_file_bytes" \
    "$max_expanded_bytes"
  rm -rf "$validation_dir"
  "$gzip_bin" -dc "$source_archive" |
    "$tar_bin" \
      --no-same-owner \
      --no-same-permissions \
      --delay-directory-restore \
      -xf - \
      -C "$extract_dir"

  top_level_count="$(
    find "$extract_dir" -mindepth 1 -maxdepth 1 -print | wc -l |
      tr -d '[:space:]'
  )"
  source_root="${extract_dir}/${SOURCE_ROOT_NAME}"
  [[ "$top_level_count" == "1" && -d "$source_root" ]] ||
    fail "source archive must contain exactly ${SOURCE_ROOT_NAME}/"
  verify_extracted_source "$node_bin" "$extract_dir" "$source_root"

  [[ -d "${source_root}/frontend" ]] ||
    fail "source release is missing frontend/"
  [[ -d "${source_root}/frontend-web" ]] ||
    fail "source release is missing frontend-web/"
  [[ -f "${source_root}/deploy/nginx/kbase.locations.conf.template" ]] ||
    fail "source release is missing the Nginx location template"
  [[ -f "${source_root}/deploy/nginx/render-kbase-config.sh" ]] ||
    fail "source release is missing the Nginx renderer"

  (
    cd "${source_root}/frontend"
    "$npm_bin" ci
    "$npm_bin" run build
  )

  shopt -s nullglob
  frontend_smokes=("${source_root}"/frontend/scripts/*-smoke.mjs)
  shopt -u nullglob
  [[ "${#frontend_smokes[@]}" -gt 0 ]] ||
    fail "source release contains no frontend smoke scripts"
  for smoke in "${frontend_smokes[@]}"; do
    (
      cd "$source_root"
      "$node_bin" "$smoke"
    )
  done

  web_app="${source_root}/frontend-web/app.js"
  [[ -f "$web_app" ]] ||
    fail "source release is missing frontend-web/app.js"
  "$node_bin" --check "$web_app"

  shopt -s nullglob
  web_smokes=("${source_root}"/frontend-web/scripts/*-smoke.mjs)
  shopt -u nullglob
  [[ "${#web_smokes[@]}" -gt 0 ]] ||
    fail "source release contains no frontend Web smoke scripts"
  for smoke in "${web_smokes[@]}"; do
    (
      cd "$source_root"
      "$node_bin" "$smoke"
    )
  done

  (
    cd "$source_root"
    "$go_bin" test ./...
  )

  candidate="${bundle_dir}/kbase-server"
  (
    cd "$source_root"
    CGO_ENABLED=1 "$go_bin" build \
      -trimpath \
      -ldflags "-X main.buildRevision=${revision}" \
      -o "$candidate" \
      ./cmd/kbase-server
  )
  chmod 0755 "$candidate"

  if [[ -z "$proxy_smoke_script" ]]; then
    proxy_smoke_script="${source_root}/deploy/nginx/browser-session-proxy-smoke.sh"
  fi
  [[ -f "$proxy_smoke_script" ]] ||
    fail "proxy smoke script does not exist: $proxy_smoke_script"
  (
    cd "$source_root"
    KBASE_SERVER_BIN="$candidate" \
      NGINX_BIN="$nginx_bin" \
      bash "$proxy_smoke_script"
  )

  "$tar_bin" \
    --sort=name \
    --mtime='@0' \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    -C "$source_root" \
    -cf - \
    frontend-web |
  "$gzip_bin" -n >"${bundle_dir}/web.tar.gz"
  chmod 0644 "${bundle_dir}/web.tar.gz"
  mkdir "$validation_dir"
  validate_prepared_web_archive \
    "$node_bin" \
    "$tar_bin" \
    "$gzip_bin" \
    "${bundle_dir}/web.tar.gz" \
    "$validation_dir"
  rm -rf "$validation_dir"

  cp \
    "${source_root}/deploy/nginx/kbase.locations.conf.template" \
    "${bundle_dir}/kbase.locations.conf.template"
  chmod 0644 "${bundle_dir}/kbase.locations.conf.template"
  cp \
    "${source_root}/deploy/nginx/render-kbase-config.sh" \
    "${bundle_dir}/render-kbase-config.sh"
  chmod 0755 "${bundle_dir}/render-kbase-config.sh"

  prepared_manifest="${temporary_dir}/${MANIFEST_NAME}"
  write_prepared_manifest \
    "$node_bin" \
    "$prepared_manifest" \
    "$revision" \
    "$bundle_dir"
  validate_prepared_release_content \
    "$prepared_manifest" \
    "$node_bin" \
    "$tar_bin" \
    "$gzip_bin"

  rm -rf "$extract_dir" "$source_input_dir"
  if [[ -e "$output_dir" || -L "$output_dir" ]]; then
    fail "output directory appeared during preparation: $output_dir"
  fi
  mv "$temporary_dir" "$output_dir"
  temporary_dir=""
  printf 'prepared release created: %s\n' "${output_dir}/${MANIFEST_NAME}"
}

validate_prepared_release_content() {
  manifest="$1"
  node_bin="$2"
  tar_bin="$3"
  gzip_bin="$4"
  "$node_bin" - \
    "$manifest" \
    "$SCHEMA" \
    "$DEFAULT_SERVER_BINARY_MAX_BYTES" \
    "$DEFAULT_WEB_ARCHIVE_MAX_COMPRESSED_BYTES" \
    "$DEFAULT_NGINX_TEMPLATE_MAX_BYTES" \
    "$DEFAULT_CONFIG_RENDERER_MAX_BYTES" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const manifestPath = path.resolve(process.argv[2]);
const expectedSchema = process.argv[3];
const serverMaximum = Number(process.argv[4]);
const webMaximum = Number(process.argv[5]);
const nginxMaximum = Number(process.argv[6]);
const rendererMaximum = Number(process.argv[7]);
const releaseDirectory = path.dirname(manifestPath);
const bundleDirectory = path.join(releaseDirectory, "bundle");
const expected = new Map([
  [
    "kbase-server",
    {
      path: "bundle/kbase-server",
      mode: "0755",
      maximum: serverMaximum,
    },
  ],
  [
    "web",
    { path: "bundle/web.tar.gz", mode: "0644", maximum: webMaximum },
  ],
  [
    "nginx-template",
    {
      path: "bundle/kbase.locations.conf.template",
      mode: "0644",
      maximum: nginxMaximum,
    },
  ],
  [
    "config-renderer",
    {
      path: "bundle/render-kbase-config.sh",
      mode: "0755",
      maximum: rendererMaximum,
    },
  ],
]);

function fail(message) {
  process.stderr.write(`prepare-release: ${message}\n`);
  process.exit(1);
}

function exactKeys(value, keys, label) {
  if (value === null || Array.isArray(value) || typeof value !== "object") {
    fail(`${label} must be a JSON object`);
  }
  const actual = Object.keys(value).sort();
  const wanted = [...keys].sort();
  if (JSON.stringify(actual) !== JSON.stringify(wanted)) {
    fail(`${label} fields must be exactly: ${wanted.join(", ")}`);
  }
}

function digest(filePath) {
  const hash = crypto.createHash("sha256");
  const descriptor = fs.openSync(filePath, "r");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  try {
    while (true) {
      const bytesRead = fs.readSync(
        descriptor,
        buffer,
        0,
        buffer.length,
        null,
      );
      if (bytesRead === 0) {
        return hash.digest("hex");
      }
      hash.update(buffer.subarray(0, bytesRead));
    }
  } finally {
    fs.closeSync(descriptor);
  }
}

let manifest;
try {
  manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
} catch (error) {
  fail(`invalid prepared manifest JSON: ${error.message}`);
}

exactKeys(manifest, ["schema", "revision", "artifacts"], "manifest");
if (manifest.schema !== expectedSchema) {
  fail(`unsupported prepared manifest schema: ${String(manifest.schema)}`);
}
if (
  typeof manifest.revision !== "string" ||
  !/^[0-9a-f]{40}$/.test(manifest.revision)
) {
  fail("prepared revision must be a canonical lowercase Git commit");
}
if (!Array.isArray(manifest.artifacts) || manifest.artifacts.length !== 4) {
  fail("prepared manifest must contain exactly four artifacts");
}

let bundleStat;
try {
  bundleStat = fs.lstatSync(bundleDirectory);
} catch (error) {
  fail("prepared bundle is missing");
}
if (!bundleStat.isDirectory() || bundleStat.isSymbolicLink()) {
  fail("prepared bundle must be a real directory");
}
const realReleaseDirectory = fs.realpathSync(releaseDirectory);
const realBundleDirectory = fs.realpathSync(bundleDirectory);
if (realBundleDirectory !== path.join(realReleaseDirectory, "bundle")) {
  fail("prepared bundle resolves outside its release directory");
}

const seen = new Set();
for (const artifact of manifest.artifacts) {
  exactKeys(artifact, ["name", "path", "sha256", "mode"], "artifact");
  if (typeof artifact.name !== "string" || !expected.has(artifact.name)) {
    fail(`unknown prepared artifact: ${String(artifact.name)}`);
  }
  if (seen.has(artifact.name)) {
    fail(`duplicate prepared artifact: ${artifact.name}`);
  }
  seen.add(artifact.name);

  const specification = expected.get(artifact.name);
  if (artifact.path !== specification.path) {
    fail(`unexpected path for prepared artifact ${artifact.name}`);
  }
  if (artifact.mode !== specification.mode || !/^0[0-7]{3}$/.test(artifact.mode)) {
    fail(`unexpected mode for prepared artifact ${artifact.name}`);
  }
  if (
    typeof artifact.sha256 !== "string" ||
    !/^[0-9a-f]{64}$/.test(artifact.sha256)
  ) {
    fail(`invalid digest for prepared artifact ${artifact.name}`);
  }
  if (
    path.posix.isAbsolute(artifact.path) ||
    path.win32.isAbsolute(artifact.path) ||
    artifact.path.includes("\\") ||
    artifact.path.split("/").some(
      (segment) => segment === "" || segment === "." || segment === "..",
    )
  ) {
    fail(`prepared artifact path is not portable: ${artifact.path}`);
  }

  const artifactPath = path.resolve(releaseDirectory, artifact.path);
  const releasePrefix = `${releaseDirectory}${path.sep}`;
  if (!artifactPath.startsWith(releasePrefix)) {
    fail(`prepared artifact escapes release directory: ${artifact.name}`);
  }

  let stat;
  try {
    stat = fs.lstatSync(artifactPath);
  } catch (error) {
    fail(`prepared artifact is missing: ${artifact.name}`);
  }
  if (!stat.isFile() || stat.isSymbolicLink()) {
    fail(`prepared artifact must be a regular file: ${artifact.name}`);
  }
  if (stat.size > specification.maximum) {
    fail(`prepared artifact exceeds byte limit: ${artifact.name}`);
  }
  const realArtifactPath = fs.realpathSync(artifactPath);
  const realReleasePrefix = `${realReleaseDirectory}${path.sep}`;
  if (!realArtifactPath.startsWith(realReleasePrefix)) {
    fail(`prepared artifact resolves outside release directory: ${artifact.name}`);
  }
  const actualMode = (stat.mode & 0o777).toString(8).padStart(4, "0");
  if (actualMode !== artifact.mode) {
    fail(`prepared artifact mode mismatch: ${artifact.name}`);
  }
  const actualDigest = digest(artifactPath);
  if (actualDigest !== artifact.sha256) {
    fail(`prepared artifact digest mismatch: ${artifact.name}`);
  }
}

if (seen.size !== expected.size) {
  fail("prepared manifest is missing a required artifact");
}

let bundleEntries;
try {
  bundleEntries = fs.readdirSync(bundleDirectory, { withFileTypes: true });
} catch (error) {
  fail(`cannot read prepared bundle: ${error.message}`);
}
const expectedBasenames = new Set(
  [...expected.values()].map((specification) => path.basename(specification.path)),
);
if (
  bundleEntries.length !== expectedBasenames.size ||
  bundleEntries.some(
    (entry) => !entry.isFile() || !expectedBasenames.has(entry.name),
  )
) {
  fail("prepared bundle contains missing or unexpected files");
}

process.stdout.write("prepared release verified\n");
NODE

  release_directory="$(cd "$(dirname "$manifest")" && pwd -P)"
  content_validation_dir="$(
    mktemp -d "${TMPDIR:-/tmp}/kbase-prepared-validation.XXXXXX"
  )"
  if ! validate_prepared_web_archive \
    "$node_bin" \
    "$tar_bin" \
    "$gzip_bin" \
    "${release_directory}/bundle/web.tar.gz" \
    "$content_validation_dir"; then
    rm -rf "$content_validation_dir"
    return 1
  fi
  rm -rf "$content_validation_dir"
}

verify_release() {
  manifest=""
  node_bin="node"
  tar_bin="tar"
  gzip_bin="gzip"
  trusted_public_key=""
  openssl_bin="openssl"

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
      --tar-bin)
        require_option_value "$1" "${2:-}"
        tar_bin="$2"
        shift 2
        ;;
      --gzip-bin)
        require_option_value "$1" "${2:-}"
        gzip_bin="$2"
        shift 2
        ;;
      --trusted-public-key)
        require_option_value "$1" "${2:-}"
        trusted_public_key="$2"
        shift 2
        ;;
      --openssl-bin)
        require_option_value "$1" "${2:-}"
        openssl_bin="$2"
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
  [[ -n "$trusted_public_key" ]] ||
    fail "verify requires --trusted-public-key"
  require_executable "$node_bin" "Node"
  require_executable "$tar_bin" "tar"
  require_executable "$gzip_bin" "gzip"
  require_executable "$openssl_bin" "OpenSSL"
  [[ -x "$SIGNATURE_HELPER" ]] ||
    fail "release signature helper is not executable"
  [[ -f "$ARCHIVE_LISTER" && ! -L "$ARCHIVE_LISTER" ]] ||
    fail "archive listing helper is missing"
  [[ -f "$manifest" && ! -L "$manifest" ]] ||
    fail "prepared manifest must be a regular file"
  [[ "${manifest##*/}" == "$MANIFEST_NAME" ]] ||
    fail "prepared manifest must use the canonical filename"
  if [[ "$manifest" == */* ]]; then
    manifest_directory="${manifest%/*}"
    [[ -n "$manifest_directory" ]] || manifest_directory="/"
  else
    manifest_directory="."
  fi
  signature="${manifest_directory}/${SIGNATURE_NAME}"
  "$SIGNATURE_HELPER" verify \
    --manifest "$manifest" \
    --signature "$signature" \
    --trusted-public-key "$trusted_public_key" \
    --openssl-bin "$openssl_bin"
  validate_prepared_release_content "$manifest" "$node_bin" "$tar_bin" "$gzip_bin"
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
    -h|--help|help) usage ;;
    *) fail "unknown operation: $operation" ;;
  esac
}

main "$@"
