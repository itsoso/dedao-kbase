#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID:-$(id -u)}" == "0" ]]; then
  PATH="/usr/sbin:/usr/bin:/sbin:/bin"
  export PATH
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREPARER="${SCRIPT_DIR}/prepare-release.sh"
WEB_ROOT_NAME="frontend-web"
MAX_WEB_ARCHIVE_BYTES=$((32 * 1024 * 1024))
MAX_WEB_MEMBERS=20000
MAX_WEB_FILE_BYTES=$((32 * 1024 * 1024))
MAX_WEB_TOTAL_BYTES=$((256 * 1024 * 1024))

transaction_active=0
rollback_in_progress=0
backup_dir=""
binary_target=""
web_target=""
env_target=""
nginx_config_target=""
service_name=""
nginx_service_name=""
health_url=""
trusted_public_key=""
node_bin="node"
openssl_bin="openssl"
tar_bin="tar"
gzip_bin="gzip"
nginx_bin="nginx"
systemctl_bin="systemctl"
curl_bin="curl"
uname_bin="uname"
binary_temp=""
web_temp=""
env_temp=""
nginx_temp=""
web_displaced=""
doctor_result=""
release_staging=""
staged_manifest=""
staged_environment=""
staged_public_key=""

usage() {
  cat <<'USAGE'
Usage:
  install-release.sh install \
    --manifest PATH \
    --trusted-public-key ABS \
    --binary-target ABS \
    --web-target ABS \
    --env-source ABS \
    --env-target ABS \
    --nginx-config-target ABS \
    --basic-auth-file ABS \
    --backend-addr LOOPBACK:PORT \
    --service-name NAME \
    --nginx-service-name NAME \
    --health-url URL \
    --backup-dir ABS \
    [--node-bin PATH] \
    [--openssl-bin PATH] \
    [--tar-bin PATH] \
    [--gzip-bin PATH] \
    [--nginx-bin PATH] \
    [--systemctl-bin PATH] \
    [--curl-bin PATH] \
    [--uname-bin PATH]
USAGE
}

die() {
  printf 'install-release: %s\n' "$*" >&2
  return 1
}

require_value() {
  local option="$1"
  local value="${2:-}"
  if [[ -z "$value" ]]; then
    die "${option} requires a value"
  fi
}

require_executable() {
  local value="$1"
  local label="$2"
  if [[ "$value" == */* ]]; then
    if [[ ! -x "$value" ]]; then
      die "${label} is not executable"
    fi
  elif ! command -v "$value" >/dev/null 2>&1; then
    die "${label} command not found"
  fi
}

parent_directory() {
  local value="$1"
  if [[ "$value" == */* ]]; then
    value="${value%/*}"
    [[ -n "$value" ]] || value="/"
  else
    value="."
  fi
  printf '%s\n' "$value"
}

base_name() {
  local value="$1"
  printf '%s\n' "${value##*/}"
}

cleanup_temporary_targets() {
  if [[ -n "$binary_temp" ]]; then
    rm -f "$binary_temp"
  fi
  if [[ -n "$web_temp" ]]; then
    rm -rf "$web_temp"
  fi
  if [[ -n "$env_temp" ]]; then
    rm -f "$env_temp"
  fi
  if [[ -n "$nginx_temp" ]]; then
    rm -f "$nginx_temp"
  fi
  if [[ -n "$doctor_result" ]]; then
    rm -f "$doctor_result"
  fi
  if [[ -n "$release_staging" ]]; then
    rm -rf "$release_staging"
    release_staging=""
  fi
}

parsed_environment=()

load_environment_arguments() {
  local environment_file="$1"
  local key
  local value
  local parsed_file
  parsed_environment=()
  parsed_file="$(mktemp "${release_staging}/environment.XXXXXX")"
  chmod 0600 "$parsed_file"
  if ! "$node_bin" - "$environment_file" >"$parsed_file" <<'NODE'
const fs = require("fs");

const environmentPath = process.argv[2];
const allowed = /^(?:KBASE_|DEDAO_|TOKENPLAN_|WECHAT_|WCPLUS|EVIDENCE_AUDIT_|PROOFROOM_)[A-Z0-9_]*$/;
const safeStandard = new Set(["HOME", "LANG", "LC_ALL", "TZ"]);
const seen = new Set();
const output = [];
const lines = fs.readFileSync(environmentPath, "utf8").split(/\n/);

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

for (let index = 0; index < lines.length; index += 1) {
  let line = lines[index];
  if (line.endsWith("\r")) {
    line = line.slice(0, -1);
  }
  const trimmed = line.trim();
  if (trimmed === "" || trimmed.startsWith("#")) {
    continue;
  }
  const separator = line.indexOf("=");
  if (separator < 1) {
    fail(`environment line ${index + 1} must be KEY=VALUE`);
  }
  const key = line.slice(0, separator).trim();
  let value = line.slice(separator + 1).trim();
  if (!/^[A-Z_][A-Z0-9_]*$/.test(key)) {
    fail(`environment line ${index + 1} has an invalid key`);
  }
  if (!allowed.test(key) && !safeStandard.has(key)) {
    fail(`environment key is not allowed: ${key}`);
  }
  if (seen.has(key)) {
    fail(`environment key is duplicated: ${key}`);
  }
  seen.add(key);
  if (
    value.length >= 2 &&
    ((value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'")))
  ) {
    value = value.slice(1, -1);
  } else if (value.startsWith('"') || value.startsWith("'")) {
    fail(`environment line ${index + 1} has an unterminated quote`);
  }
  if ([...value].some((character) => {
    const code = character.codePointAt(0);
    return code === 0 || code === 10 || code === 13;
  })) {
    fail(`environment line ${index + 1} contains a control character`);
  }
  output.push(key, value);
}

process.stdout.write(`${output.join("\0")}${output.length > 0 ? "\0" : ""}`);
NODE
  then
    rm -f "$parsed_file"
    return 1
  fi
  while IFS= read -r -d '' key && IFS= read -r -d '' value; do
    parsed_environment+=("${key}=${value}")
  done <"$parsed_file"
  rm -f "$parsed_file"
}

run_with_environment() {
  local environment_file="$1"
  shift
  load_environment_arguments "$environment_file"
  env "${parsed_environment[@]}" "$@"
}

run_renderer() {
  local environment_file="$1"
  local renderer="$2"
  local template="$3"
  local output="$4"
  local backend="$5"
  local auth_file="$6"
  load_environment_arguments "$environment_file"
  env \
    "${parsed_environment[@]}" \
    "KBASE_BACKEND_ADDR=$backend" \
    "KBASE_BASIC_AUTH_FILE=$auth_file" \
    "$renderer" \
    "$template" \
    "$output"
}

validate_doctor_result() {
  local result_file="$1"
  "$node_bin" - "$result_file" <<'NODE'
const fs = require("fs");

const resultPath = process.argv[2];
let result;
try {
  result = JSON.parse(fs.readFileSync(resultPath, "utf8"));
} catch {
  process.stderr.write("install-release: configuration doctor returned invalid JSON\n");
  process.exit(1);
}
if (
  result === null ||
  Array.isArray(result) ||
  typeof result !== "object" ||
  JSON.stringify(Object.keys(result).sort()) !==
    JSON.stringify(["schema_version", "status"]) ||
  result.schema_version !== 1 ||
  result.status !== "ok"
) {
  process.stderr.write(
    "install-release: configuration doctor returned an invalid result\n",
  );
  process.exit(1);
}
NODE
}

retry_health() {
  local attempts="$1"
  local url="$2"
  local index=1
  local response_file="${release_staging}/health-response.json"
  while ((index <= attempts)); do
    if "$curl_bin" \
      --fail \
      --silent \
      --show-error \
      --max-time 2 \
      "$url" >"$response_file" &&
      "$node_bin" - "$response_file" <<'NODE'
const fs = require("fs");
let value;
try {
  value = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
} catch {
  process.exit(1);
}
if (
  value === null ||
  Array.isArray(value) ||
  typeof value !== "object" ||
  JSON.stringify(Object.keys(value).sort()) !==
    JSON.stringify(["ok", "service"]) ||
  value.ok !== true ||
  value.service !== "dedao-kbase"
) {
  process.exit(1);
}
NODE
    then
      rm -f "$response_file"
      return 0
    fi
    rm -f "$response_file"
    if ((index < attempts)); then
      sleep 0.1
    fi
    index=$((index + 1))
  done
  return 1
}

prepare_restore_file() {
  local snapshot="$1"
  local target="$2"
  local variable_name="$3"
  local parent
  local name
  local temporary
  parent="$(parent_directory "$target")"
  name="$(base_name "$target")"
  temporary="$(mktemp "${parent}/.${name}.kbase-install.restore.XXXXXX")"
  if ! cp -p "$snapshot" "$temporary"; then
    rm -f "$temporary"
    return 1
  fi
  printf -v "$variable_name" '%s' "$temporary"
}

prepare_restore_directory() {
  local snapshot="$1"
  local target="$2"
  local variable_name="$3"
  local parent
  local name
  local temporary
  parent="$(parent_directory "$target")"
  name="$(base_name "$target")"
  temporary="$(mktemp -d "${parent}/.${name}.kbase-install.restore.XXXXXX")"
  if ! cp -a "${snapshot}/." "$temporary"; then
    rm -rf "$temporary"
    return 1
  fi
  if ! "$node_bin" - "$snapshot" "$temporary" <<'NODE'
const fs = require("fs");
fs.chmodSync(process.argv[3], fs.statSync(process.argv[2]).mode & 0o777);
NODE
  then
    rm -rf "$temporary"
    return 1
  fi
  printf -v "$variable_name" '%s' "$temporary"
}

rollback_transaction() {
  local failed=0
  local restore_binary=""
  local restore_web=""
  local restore_env=""
  local restore_nginx=""
  local snapshot="${backup_dir}/snapshot"

  rollback_in_progress=1
  printf 'install-release: rolling back previous release\n' >&2

  if ! prepare_restore_file \
    "${snapshot}/kbase-server" \
    "$binary_target" \
    restore_binary; then
    printf 'install-release: rollback could not prepare binary snapshot\n' >&2
    failed=1
  fi
  if ! prepare_restore_directory \
    "${snapshot}/web" \
    "$web_target" \
    restore_web; then
    printf 'install-release: rollback could not prepare web snapshot\n' >&2
    failed=1
  fi
  if ! prepare_restore_file \
    "${snapshot}/service.env" \
    "$env_target" \
    restore_env; then
    printf 'install-release: rollback could not prepare environment snapshot\n' >&2
    failed=1
  fi
  if ! prepare_restore_file \
    "${snapshot}/nginx.conf" \
    "$nginx_config_target" \
    restore_nginx; then
    printf 'install-release: rollback could not prepare Nginx snapshot\n' >&2
    failed=1
  fi

  if [[ "$failed" == "0" ]]; then
    if mv -f "$restore_binary" "$binary_target"; then
      restore_binary=""
    else
      printf 'install-release: rollback failed to restore binary\n' >&2
      failed=1
    fi

    if rm -rf "$web_target" && mv "$restore_web" "$web_target"; then
      restore_web=""
    else
      printf 'install-release: rollback failed to restore web directory\n' >&2
      failed=1
    fi

    if mv -f "$restore_env" "$env_target"; then
      restore_env=""
    else
      printf 'install-release: rollback failed to restore environment\n' >&2
      failed=1
    fi

    if mv -f "$restore_nginx" "$nginx_config_target"; then
      restore_nginx=""
    else
      printf 'install-release: rollback failed to restore Nginx config\n' >&2
      failed=1
    fi
  fi

  [[ -z "$restore_binary" ]] || rm -f "$restore_binary"
  [[ -z "$restore_web" ]] || rm -rf "$restore_web"
  [[ -z "$restore_env" ]] || rm -f "$restore_env"
  [[ -z "$restore_nginx" ]] || rm -f "$restore_nginx"

  if ! "$systemctl_bin" restart "$service_name"; then
    printf 'install-release: rollback service restart failed\n' >&2
    failed=1
  fi
  if ! "$nginx_bin" -t; then
    printf 'install-release: rollback Nginx validation failed\n' >&2
    failed=1
  fi
  if ! "$systemctl_bin" reload "$nginx_service_name"; then
    printf 'install-release: rollback Nginx reload failed\n' >&2
    failed=1
  fi
  if ! retry_health 5 "$health_url"; then
    printf 'install-release: rollback health check failed\n' >&2
    failed=1
  fi

  cleanup_temporary_targets
  if [[ "$failed" != "0" ]]; then
    if [[ -n "$web_displaced" && -d "$web_displaced" ]]; then
      printf 'install-release: displaced Web retained at %s\n' \
        "$web_displaced" >&2
    fi
    printf 'install-release: rollback incomplete; backup retained\n' >&2
    return 1
  fi

  if [[ -n "$web_displaced" ]]; then
    rm -rf "$web_displaced"
    web_displaced=""
  fi
  transaction_active=0
  printf 'install-release: rollback complete; backup retained\n' >&2
  return 0
}

handle_error() {
  local status=$?
  trap - ERR
  set +e
  printf 'install-release: install failed with status %s\n' "$status" >&2
  if [[ "$transaction_active" == "1" && "$rollback_in_progress" == "0" ]]; then
    if ! rollback_transaction; then
      exit 70
    fi
  else
    cleanup_temporary_targets
  fi
  exit "$status"
}

handle_signal() {
  local signal_name="$1"
  local status="$2"
  trap - ERR HUP INT TERM
  set +e
  printf 'install-release: interrupted by %s\n' "$signal_name" >&2
  if [[ "$transaction_active" == "1" && "$rollback_in_progress" == "0" ]]; then
    if ! rollback_transaction; then
      exit 70
    fi
  else
    cleanup_temporary_targets
  fi
  exit "$status"
}

trap handle_error ERR
trap 'handle_signal HUP 129' HUP
trap 'handle_signal INT 130' INT
trap 'handle_signal TERM 143' TERM

validate_paths_and_inputs() {
  local manifest="$1"
  local env_source="$2"
  local basic_auth_file="$3"
  local public_key="$4"
  local backup_parent
  local target_parent

  "$node_bin" - \
    "$manifest" \
    "$binary_target" \
    "$web_target" \
    "$env_source" \
    "$env_target" \
    "$nginx_config_target" \
    "$basic_auth_file" \
    "$public_key" \
    "$backup_dir" \
    "$health_url" <<'NODE'
const fs = require("fs");
const path = require("path");

const [
  manifest,
  binaryTarget,
  webTarget,
  envSource,
  envTarget,
  nginxTarget,
  basicAuth,
  publicKey,
  backupDirectory,
  healthUrl,
] = process.argv.slice(2);

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

const absolutePaths = [
  ["manifest", manifest],
  ["binary target", binaryTarget],
  ["web target", webTarget],
  ["environment source", envSource],
  ["environment target", envTarget],
  ["Nginx config target", nginxTarget],
  ["basic auth file", basicAuth],
  ["trusted public key", publicKey],
  ["backup directory", backupDirectory],
];
for (const [label, value] of absolutePaths) {
  if (!path.isAbsolute(value)) {
    fail(`${label} must be absolute`);
  }
}

function regular(pathname, label) {
  const stat = fs.lstatSync(pathname);
  if (stat.isSymbolicLink() || !stat.isFile()) {
    fail(`${label} must be a real regular file`);
  }
  return stat;
}

regular(manifest, "manifest");
regular(binaryTarget, "binary target");
const webStat = fs.lstatSync(webTarget);
if (webStat.isSymbolicLink() || !webStat.isDirectory()) {
  fail("web target must be a real directory");
}
const sourceStat = regular(envSource, "environment source");
regular(envTarget, "environment target");
regular(nginxTarget, "Nginx config target");
regular(basicAuth, "basic auth file");
regular(publicKey, "trusted public key");

if ((sourceStat.mode & 0o022) !== 0) {
  fail("environment source must not be group/other writable");
}

function trustedPath(pathname, label, finalType) {
  const currentUid = typeof process.getuid === "function" ? process.getuid() : 0;
  const normalized = path.resolve(pathname);
  const segments = normalized.split(path.sep).filter(Boolean);
  let cursor = path.parse(normalized).root;
  for (let index = 0; index < segments.length; index += 1) {
    cursor = path.join(cursor, segments[index]);
    const stat = fs.lstatSync(cursor);
    if (stat.isSymbolicLink()) {
      fail(`${label} path must not contain symbolic links`);
    }
    const isFile = index === segments.length - 1;
    const validType = isFile
      ? finalType === "file"
        ? stat.isFile()
        : stat.isDirectory()
      : stat.isDirectory();
    if (!validType) {
      fail(`${label} path has an invalid component`);
    }
    if (currentUid === 0 ? stat.uid !== 0 : stat.uid !== 0 && stat.uid !== currentUid) {
      fail(`${label} path has an untrusted owner`);
    }
    if ((stat.mode & 0o022) !== 0) {
      const trustedStickyDirectory =
        !isFile && stat.uid === 0 && (stat.mode & 0o1000) !== 0;
      if (!trustedStickyDirectory) {
        fail(`${label} path is group/other writable`);
      }
    }
  }
}

trustedPath(envSource, "environment source", "file");
trustedPath(basicAuth, "basic auth file", "file");
trustedPath(publicKey, "trusted public key", "file");
trustedPath(path.dirname(backupDirectory), "backup parent", "directory");

const targets = [binaryTarget, webTarget, envTarget, nginxTarget].map(
  (target) => fs.realpathSync(target),
);
const identities = targets.map((target) => {
  const stat = fs.statSync(target);
  return `${stat.dev}:${stat.ino}`;
});
if (new Set(identities).size !== identities.length) {
  fail("installation targets must be distinct");
}

function contains(parent, child) {
  const relative = path.relative(parent, child);
  return (
    relative === "" ||
    (relative !== ".." &&
      !relative.startsWith(`..${path.sep}`) &&
      !path.isAbsolute(relative))
  );
}

for (let left = 0; left < targets.length; left += 1) {
  for (let right = left + 1; right < targets.length; right += 1) {
    if (
      contains(targets[left], targets[right]) ||
      contains(targets[right], targets[left])
    ) {
      fail("installation targets must not contain one another");
    }
  }
}
if (fs.realpathSync(envSource) === fs.realpathSync(envTarget)) {
  fail("environment source must differ from environment target");
}

if (fs.existsSync(backupDirectory) || fs.lstatSync(path.dirname(backupDirectory)).isSymbolicLink()) {
  fail("backup directory must not exist and its parent must be real");
}
const backupParent = fs.lstatSync(path.dirname(backupDirectory));
if (!backupParent.isDirectory()) {
  fail("backup parent must be a directory");
}
const realBackupDirectory = path.join(
  fs.realpathSync(path.dirname(backupDirectory)),
  path.basename(backupDirectory),
);
for (const target of targets) {
  if (
    contains(target, realBackupDirectory) ||
    contains(realBackupDirectory, target)
  ) {
    fail("backup directory must not overlap an installation target");
  }
}

let parsed;
try {
  parsed = new URL(healthUrl);
} catch {
  fail("health URL is invalid");
}
if (
  (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
  parsed.username !== "" ||
  parsed.password !== ""
) {
  fail("health URL must be HTTP(S) without credentials");
}
NODE

  for target_parent in \
    "$(parent_directory "$binary_target")" \
    "$(parent_directory "$web_target")" \
    "$(parent_directory "$env_target")" \
    "$(parent_directory "$nginx_config_target")"; do
    if [[ ! -d "$target_parent" || ! -w "$target_parent" ]]; then
      die "target parent must exist and be writable"
    fi
  done
  backup_parent="$(parent_directory "$backup_dir")"
  if [[ ! -d "$backup_parent" || ! -w "$backup_parent" ]]; then
    die "backup parent must exist and be writable"
  fi
}

validate_web_archive() {
  local archive="$1"
  local validation_dir="$2"
  local names_file="${validation_dir}/web-members.txt"
  local verbose_file="${validation_dir}/web-members.verbose.txt"

  "$node_bin" - "$archive" "$MAX_WEB_ARCHIVE_BYTES" <<'NODE'
const fs = require("fs");
const archiveBytes = BigInt(fs.statSync(process.argv[2]).size);
const maximum = BigInt(process.argv[3]);
if (archiveBytes > maximum) {
  process.stderr.write(
    `install-release: web archive exceeds the compressed size limit: ${archiveBytes} > ${maximum}\n`,
  );
  process.exit(1);
}
NODE

  "$gzip_bin" -dc "$archive" |
    "$tar_bin" --quoting-style=escape -tf - >"$names_file"
  "$gzip_bin" -dc "$archive" |
    "$tar_bin" --quoting-style=escape -tvf - >"$verbose_file"

  "$node_bin" - \
    "$archive" \
    "$names_file" \
    "$verbose_file" \
    "$WEB_ROOT_NAME" \
    "$MAX_WEB_ARCHIVE_BYTES" \
    "$MAX_WEB_MEMBERS" \
    "$MAX_WEB_FILE_BYTES" \
    "$MAX_WEB_TOTAL_BYTES" <<'NODE'
const fs = require("fs");

const archivePath = process.argv[2];
const namesPath = process.argv[3];
const verbosePath = process.argv[4];
const expectedRoot = process.argv[5];
const maxArchiveBytes = Number(process.argv[6]);
const maxMembers = Number(process.argv[7]);
const maxFileBytes = Number(process.argv[8]);
const maxTotalBytes = Number(process.argv[9]);

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

function lines(filePath) {
  const values = fs.readFileSync(filePath, "utf8").split("\n");
  if (values[values.length - 1] === "") {
    values.pop();
  }
  return values;
}

const names = lines(namesPath);
const verbose = lines(verbosePath);
if (names.length === 0 || names.length !== verbose.length) {
  fail("web archive member listing is empty or inconsistent");
}
if (fs.statSync(archivePath).size > maxArchiveBytes) {
  fail("web archive exceeds the compressed size limit");
}
if (names.length > maxMembers) {
  fail("web archive exceeds the member count limit");
}

function memberSize(value) {
  const gnu = value.match(/^\S+\s+\S+\/\S+\s+(\d+)\s+/);
  if (gnu) {
    return Number(gnu[1]);
  }
  const bsd = value.match(/^\S+\s+\d+\s+\S+\s+\S+\s+(\d+)\s+/);
  if (bsd) {
    return Number(bsd[1]);
  }
  fail("web archive has an unrecognized verbose listing");
}

let totalBytes = 0;
for (let index = 0; index < names.length; index += 1) {
  const name = names[index];
  if (
    name.length === 0 ||
    name.startsWith("/") ||
    name.includes("\\") ||
    [...name].some((character) => {
      const code = character.codePointAt(0);
      return code < 0x20 || code === 0x7f;
    })
  ) {
    fail(`web archive contains an unsafe member path: ${name}`);
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
  const type = verbose[index][0];
  if (type !== "-" && type !== "d") {
    fail(`web archive member type is not allowed: ${name}`);
  }
  const size = memberSize(verbose[index]);
  if (!Number.isSafeInteger(size) || size < 0) {
    fail(`web archive member has an invalid size: ${name}`);
  }
  if (type === "-" && size > maxFileBytes) {
    fail(`web archive member exceeds the file size limit: ${name}`);
  }
  totalBytes += size;
  if (!Number.isSafeInteger(totalBytes) || totalBytes > maxTotalBytes) {
    fail("web archive exceeds the expanded size limit");
  }
}
NODE
}

verify_extracted_web() {
  local candidate_web="$1"
  local unsafe_entry
  unsafe_entry="$(
    find "$candidate_web" \
      -mindepth 1 \
      ! -type f \
      ! -type d \
      -print \
      -quit
  )"
  if [[ -n "$unsafe_entry" ]]; then
    die "extracted web bundle contains a non-file/non-directory entry"
  fi
  if [[ -z "$(find "$candidate_web" -mindepth 1 -print -quit)" ]]; then
    die "extracted web bundle is empty"
  fi
}

snapshot_targets() {
  local snapshot="${backup_dir}/snapshot"
  mkdir -m 0700 "$snapshot"
  cp -p "$binary_target" "${snapshot}/kbase-server"
  cp -a "$web_target" "${snapshot}/web"
  cp -p "$env_target" "${snapshot}/service.env"
  cp -p "$nginx_config_target" "${snapshot}/nginx.conf"
}

prepare_target_temporary_files() {
  local candidate_binary="$1"
  local candidate_web="$2"
  local env_source="$3"
  local candidate_nginx="$4"
  local parent
  local name

  parent="$(parent_directory "$binary_target")"
  name="$(base_name "$binary_target")"
  binary_temp="$(mktemp "${parent}/.${name}.kbase-install.XXXXXX")"
  cp "$candidate_binary" "$binary_temp"
  chmod 0755 "$binary_temp"

  parent="$(parent_directory "$web_target")"
  name="$(base_name "$web_target")"
  web_temp="$(mktemp -d "${parent}/.${name}.kbase-install.XXXXXX")"
  cp -a "${candidate_web}/." "$web_temp"
  chmod 0755 "$web_temp"
  web_displaced="$(mktemp -d "${parent}/.${name}.kbase-install.previous.XXXXXX")"
  rmdir "$web_displaced"

  parent="$(parent_directory "$env_target")"
  name="$(base_name "$env_target")"
  env_temp="$(mktemp "${parent}/.${name}.kbase-install.XXXXXX")"
  cp "$env_source" "$env_temp"
  chmod 0600 "$env_temp"

  parent="$(parent_directory "$nginx_config_target")"
  name="$(base_name "$nginx_config_target")"
  nginx_temp="$(mktemp "${parent}/.${name}.kbase-install.XXXXXX")"
  cp "$candidate_nginx" "$nginx_temp"
  chmod 0600 "$nginx_temp"
}

stage_prepared_release() {
  local manifest="$1"
  local env_source="$2"
  local public_key="$3"
  local original_release
  local staged_release
  local name

  [[ "$(base_name "$manifest")" == "prepared-manifest.json" ]] ||
    die "prepared manifest must use the canonical filename"
  original_release="$(cd "$(dirname "$manifest")" && pwd -P)"
  staged_release="${release_staging}/release"
  mkdir -m 0700 "$staged_release" "${staged_release}/bundle"

  for name in \
    prepared-manifest.json \
    MANIFEST.sig \
    bundle/kbase-server \
    bundle/web.tar.gz \
    bundle/kbase.locations.conf.template \
    bundle/render-kbase-config.sh; do
    if [[ ! -f "${original_release}/${name}" || -L "${original_release}/${name}" ]]; then
      die "prepared release input must be a regular file: $name"
    fi
    cp "${original_release}/${name}" "${staged_release}/${name}"
  done
  chmod 0600 \
    "${staged_release}/prepared-manifest.json" \
    "${staged_release}/MANIFEST.sig"
  chmod 0755 \
    "${staged_release}/bundle/kbase-server" \
    "${staged_release}/bundle/render-kbase-config.sh"
  chmod 0644 \
    "${staged_release}/bundle/web.tar.gz" \
    "${staged_release}/bundle/kbase.locations.conf.template"

  staged_public_key="${release_staging}/trusted-release-public-key.pem"
  cp "$public_key" "$staged_public_key"
  chmod 0600 "$staged_public_key"
  staged_environment="${release_staging}/candidate.env"
  cp "$env_source" "$staged_environment"
  chmod 0600 "$staged_environment"
  staged_manifest="${staged_release}/prepared-manifest.json"

  "$PREPARER" verify \
    --node-bin "$node_bin" \
    --openssl-bin "$openssl_bin" \
    --trusted-public-key "$staged_public_key" \
    --manifest "$staged_manifest" \
    >/dev/null
}

install_release() {
  local manifest="$1"
  local env_source="$2"
  local basic_auth_file="$3"
  local backend_addr="$4"
  local release_dir
  local bundle_dir
  local staging
  local candidate_binary
  local candidate_web
  local candidate_nginx
  local template
  local renderer

  if [[ "$("$uname_bin" -s)" != "Linux" ]]; then
    die "install is supported only on Linux"
  fi

  if [[ ! "$service_name" =~ ^[A-Za-z0-9_.@-]+$ ]]; then
    die "service name contains unsafe characters"
  fi
  if [[ ! "$nginx_service_name" =~ ^[A-Za-z0-9_.@-]+$ ]]; then
    die "Nginx service name contains unsafe characters"
  fi
  if [[ ! "$backend_addr" =~ ^(127\.0\.0\.1|localhost|\[::1\]):[0-9]{1,5}$ ]]; then
    die "backend address must use a loopback host and port"
  fi
  local backend_port="${backend_addr##*:}"
  if ((10#$backend_port < 1 || 10#$backend_port > 65535)); then
    die "backend port must be between 1 and 65535"
  fi
  local expected_health_url="http://${backend_addr}/health"
  if [[ "$health_url" != "$expected_health_url" ]]; then
    die "health URL must be the selected loopback backend /health endpoint"
  fi

  validate_paths_and_inputs \
    "$manifest" \
    "$env_source" \
    "$basic_auth_file" \
    "$trusted_public_key"

  (umask 077; mkdir "$backup_dir")
  chmod 0700 "$backup_dir"
  staging="${backup_dir}/staging"
  mkdir -m 0700 "$staging"
  release_staging="$staging"
  stage_prepared_release "$manifest" "$env_source" "$trusted_public_key"

  release_dir="$(cd "$(dirname "$staged_manifest")" && pwd -P)"
  bundle_dir="${release_dir}/bundle"
  candidate_binary="${staging}/kbase-server"
  candidate_web="${staging}/web"
  candidate_nginx="${staging}/kbase.locations.conf"
  template="${bundle_dir}/kbase.locations.conf.template"
  renderer="${bundle_dir}/render-kbase-config.sh"

  cp "${bundle_dir}/kbase-server" "$candidate_binary"
  chmod 0755 "$candidate_binary"
  mkdir -m 0755 "$candidate_web"
  validate_web_archive \
    "${bundle_dir}/web.tar.gz" \
    "$staging"
  "$gzip_bin" -dc "${bundle_dir}/web.tar.gz" |
    "$tar_bin" -xf - -C "$candidate_web" --strip-components=1
  verify_extracted_web "$candidate_web"

  doctor_result="${staging}/doctor-result.json"
  run_with_environment \
    "$staged_environment" \
    "$candidate_binary" \
    --check-config \
    --web-dir \
    "$candidate_web" \
    >"$doctor_result"
  validate_doctor_result "$doctor_result"
  rm -f "$doctor_result"
  doctor_result=""

  run_renderer \
    "$staged_environment" \
    "$renderer" \
    "$template" \
    "$candidate_nginx" \
    "$backend_addr" \
    "$basic_auth_file"
  if [[ ! -f "$candidate_nginx" || -L "$candidate_nginx" ]]; then
    die "Nginx renderer did not produce a regular candidate config"
  fi
  chmod 0600 "$candidate_nginx"

  snapshot_targets
  prepare_target_temporary_files \
    "$candidate_binary" \
    "$candidate_web" \
    "$staged_environment" \
    "$candidate_nginx"

  transaction_active=1
  mv -f "$binary_temp" "$binary_target"
  binary_temp=""
  mv "$web_target" "$web_displaced"
  mv "$web_temp" "$web_target"
  web_temp=""
  mv -f "$env_temp" "$env_target"
  env_temp=""
  mv -f "$nginx_temp" "$nginx_config_target"
  nginx_temp=""

  "$systemctl_bin" restart "$service_name"
  retry_health 20 "$health_url"
  "$nginx_bin" -t
  "$systemctl_bin" reload "$nginx_service_name"
  retry_health 20 "$health_url"

  transaction_active=0
  rm -rf "$web_displaced"
  web_displaced=""
  rm -rf "$staging"
  release_staging=""
  printf '{"schema_version":1,"status":"installed","backup":"retained"}\n'
}

main() {
  local command="${1:-}"
  local manifest=""
  local env_source=""
  local basic_auth_file=""
  local backend_addr=""

  if [[ "$command" != "install" ]]; then
    usage >&2
    die "expected install command"
  fi
  shift

  while (($# > 0)); do
    case "$1" in
      --manifest)
        require_value "$1" "${2:-}"
        manifest="$2"
        shift 2
        ;;
      --trusted-public-key)
        require_value "$1" "${2:-}"
        trusted_public_key="$2"
        shift 2
        ;;
      --binary-target)
        require_value "$1" "${2:-}"
        binary_target="$2"
        shift 2
        ;;
      --web-target)
        require_value "$1" "${2:-}"
        web_target="$2"
        shift 2
        ;;
      --env-source)
        require_value "$1" "${2:-}"
        env_source="$2"
        shift 2
        ;;
      --env-target)
        require_value "$1" "${2:-}"
        env_target="$2"
        shift 2
        ;;
      --nginx-config-target)
        require_value "$1" "${2:-}"
        nginx_config_target="$2"
        shift 2
        ;;
      --basic-auth-file)
        require_value "$1" "${2:-}"
        basic_auth_file="$2"
        shift 2
        ;;
      --backend-addr)
        require_value "$1" "${2:-}"
        backend_addr="$2"
        shift 2
        ;;
      --service-name)
        require_value "$1" "${2:-}"
        service_name="$2"
        shift 2
        ;;
      --nginx-service-name)
        require_value "$1" "${2:-}"
        nginx_service_name="$2"
        shift 2
        ;;
      --health-url)
        require_value "$1" "${2:-}"
        health_url="$2"
        shift 2
        ;;
      --backup-dir)
        require_value "$1" "${2:-}"
        backup_dir="$2"
        shift 2
        ;;
      --node-bin)
        require_value "$1" "${2:-}"
        node_bin="$2"
        shift 2
        ;;
      --openssl-bin)
        require_value "$1" "${2:-}"
        openssl_bin="$2"
        shift 2
        ;;
      --tar-bin)
        require_value "$1" "${2:-}"
        tar_bin="$2"
        shift 2
        ;;
      --gzip-bin)
        require_value "$1" "${2:-}"
        gzip_bin="$2"
        shift 2
        ;;
      --nginx-bin)
        require_value "$1" "${2:-}"
        nginx_bin="$2"
        shift 2
        ;;
      --systemctl-bin)
        require_value "$1" "${2:-}"
        systemctl_bin="$2"
        shift 2
        ;;
      --curl-bin)
        require_value "$1" "${2:-}"
        curl_bin="$2"
        shift 2
        ;;
      --uname-bin)
        require_value "$1" "${2:-}"
        uname_bin="$2"
        shift 2
        ;;
      -h|--help)
        usage
        return 0
        ;;
      *)
        die "unknown option: $1"
        ;;
    esac
  done

  for value in \
    "$manifest" \
    "$trusted_public_key" \
    "$binary_target" \
    "$web_target" \
    "$env_source" \
    "$env_target" \
    "$nginx_config_target" \
    "$basic_auth_file" \
    "$backend_addr" \
    "$service_name" \
    "$nginx_service_name" \
    "$health_url" \
    "$backup_dir"; do
    if [[ -z "$value" ]]; then
      usage >&2
      die "all install options are required"
    fi
  done

  if [[ ! -x "$PREPARER" ]]; then
    die "prepare-release.sh is missing or not executable"
  fi
  require_executable "$node_bin" "Node"
  require_executable "$openssl_bin" "OpenSSL"
  require_executable "$tar_bin" "tar"
  require_executable "$gzip_bin" "gzip"
  require_executable "$nginx_bin" "Nginx"
  require_executable "$systemctl_bin" "systemctl"
  require_executable "$curl_bin" "curl"
  require_executable "$uname_bin" "uname"

  install_release "$manifest" "$env_source" "$basic_auth_file" "$backend_addr"
}

main "$@"
