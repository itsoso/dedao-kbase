#!/usr/bin/env bash
set -Eeuo pipefail

unset \
  BASH_ENV \
  CDPATH \
  DYLD_FALLBACK_LIBRARY_PATH \
  DYLD_INSERT_LIBRARIES \
  DYLD_LIBRARY_PATH \
  ENV \
  GZIP \
  LD_LIBRARY_PATH \
  LD_PRELOAD \
  NODE_OPTIONS \
  NODE_PATH \
  OPENSSL_CONF \
  OPENSSL_MODULES \
  PYTHONHOME \
  PYTHONPATH \
  TAR_OPTIONS \
  TAPE

if [[ "${EUID:-$(id -u)}" == "0" ]]; then
  PATH="/usr/sbin:/usr/bin:/sbin:/bin"
  export PATH
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREPARER="${SCRIPT_DIR}/prepare-release.sh"
SIGNATURE_HELPER="${SCRIPT_DIR}/release-signature.sh"
ARCHIVE_LISTER="${SCRIPT_DIR}/archive-list.mjs"
FSYNC_HELPER="${SCRIPT_DIR}/fsync-paths.mjs"
STAGE_HELPER="${SCRIPT_DIR}/stage-files.mjs"
WEB_ROOT_NAME="frontend-web"
TRUSTED_EXEC_PATH="/usr/sbin:/usr/bin:/sbin:/bin"
TRUSTED_RELEASE_TOOL_DIR="/opt/dedao-kbase/release-tools"
MAX_WEB_ARCHIVE_BYTES=$((32 * 1024 * 1024))
MAX_WEB_MEMBERS=20000
MAX_WEB_FILE_BYTES=$((32 * 1024 * 1024))
MAX_WEB_TOTAL_BYTES=$((256 * 1024 * 1024))
MAX_PREPARED_MANIFEST_BYTES=$((1 * 1024 * 1024))
MAX_RELEASE_SIGNATURE_BYTES=$((64 * 1024))
MAX_SERVER_BINARY_BYTES=$((256 * 1024 * 1024))
MAX_NGINX_TEMPLATE_BYTES=$((1 * 1024 * 1024))
MAX_CONFIG_RENDERER_BYTES=$((1 * 1024 * 1024))
MAX_PUBLIC_KEY_BYTES=$((64 * 1024))
MAX_ENVIRONMENT_BYTES=$((1 * 1024 * 1024))
ARCHIVE_LIST_TIMEOUT_MS=30000
ARCHIVE_LIST_STDOUT_LIMIT_BYTES=4194304
ARCHIVE_LIST_STDERR_LIMIT_BYTES=1048576

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
public_health_url=""
candidate_revision=""
previous_revision=""
transaction_state_file=""
trusted_public_key=""
node_bin="node"
openssl_bin="openssl"
tar_bin="tar"
gzip_bin="gzip"
nginx_bin="nginx"
systemctl_bin="systemctl"
curl_bin="curl"
flock_bin="flock"
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
    --public-health-url URL \
    --backup-dir ABS \
    --transaction-state-file ABS \
    [--node-bin PATH] \
    [--openssl-bin PATH] \
    [--tar-bin PATH] \
    [--gzip-bin PATH] \
    [--nginx-bin PATH] \
    [--systemctl-bin PATH] \
    [--curl-bin PATH] \
    [--flock-bin PATH] \
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

canonical_executable() {
  local value="$1"
  local label="$2"
  local resolved
  if [[ "$value" == */* ]]; then
    resolved="$(/usr/bin/realpath -e -- "$value")" ||
      die "${label} cannot be resolved: $value"
  else
    resolved="$(builtin type -P -- "$value")" ||
      die "${label} command not found: $value"
    resolved="$(/usr/bin/realpath -e -- "$resolved")" ||
      die "${label} cannot be resolved: $value"
  fi
  [[ -x "$resolved" ]] || die "${label} is not executable: $resolved"
  printf '%s\n' "$resolved"
}

validate_privileged_runtime() {
  [[ "${EUID:-$(id -u)}" == "0" ]] || return 0
  [[ "$SCRIPT_DIR" == "$TRUSTED_RELEASE_TOOL_DIR" ]] ||
    die "root installation requires release tools at ${TRUSTED_RELEASE_TOOL_DIR}"
  if [[ "$node_bin" != "node" ||
    "$openssl_bin" != "openssl" ||
    "$tar_bin" != "tar" ||
    "$gzip_bin" != "gzip" ||
    "$nginx_bin" != "nginx" ||
    "$systemctl_bin" != "systemctl" ||
    "$curl_bin" != "curl" ||
    "$flock_bin" != "flock" ||
    "$uname_bin" != "uname" ]]; then
    die "root installation does not accept executable overrides"
  fi

  node_bin="$(canonical_executable "$node_bin" "Node")"
  openssl_bin="$(canonical_executable "$openssl_bin" "OpenSSL")"
  tar_bin="$(canonical_executable "$tar_bin" "tar")"
  gzip_bin="$(canonical_executable "$gzip_bin" "gzip")"
  nginx_bin="$(canonical_executable "$nginx_bin" "Nginx")"
  systemctl_bin="$(canonical_executable "$systemctl_bin" "systemctl")"
  curl_bin="$(canonical_executable "$curl_bin" "curl")"
  flock_bin="$(canonical_executable "$flock_bin" "flock")"
  uname_bin="$(canonical_executable "$uname_bin" "uname")"

  "$node_bin" - \
    "$SCRIPT_DIR" \
    "${SCRIPT_DIR}/install-release.sh" \
    "$PREPARER" \
    "$SIGNATURE_HELPER" \
    "$ARCHIVE_LISTER" \
    "$FSYNC_HELPER" \
    "$STAGE_HELPER" \
    "$node_bin" \
    "$openssl_bin" \
    "$tar_bin" \
    "$gzip_bin" \
    "$nginx_bin" \
    "$systemctl_bin" \
    "$curl_bin" \
    "$flock_bin" \
    "$uname_bin" <<'NODE'
const fs = require("fs");
const path = require("path");

const [toolDirectory, ...files] = process.argv.slice(2);

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

function validate(pathname, finalType, label) {
  if (!path.isAbsolute(pathname)) {
    fail(`${label} path must be absolute`);
  }
  const normalized = path.resolve(pathname);
  const segments = normalized.split(path.sep).filter(Boolean);
  let cursor = path.parse(normalized).root;
  for (let index = 0; index < segments.length; index += 1) {
    cursor = path.join(cursor, segments[index]);
    const stat = fs.lstatSync(cursor);
    const final = index === segments.length - 1;
    if (stat.isSymbolicLink()) {
      fail(`${label} path must not contain symbolic links`);
    }
    if (
      final
        ? finalType === "file"
          ? !stat.isFile()
          : !stat.isDirectory()
        : !stat.isDirectory()
    ) {
      fail(`${label} path has an invalid component`);
    }
    if (stat.uid !== 0) {
      fail(`${label} path must be root-owned`);
    }
    if ((stat.mode & 0o022) !== 0) {
      fail(`${label} path must not be group/other writable`);
    }
  }
}

validate(toolDirectory, "directory", "release tool directory");
for (const pathname of files) {
  validate(pathname, "file", `privileged tool ${pathname}`);
}
NODE
}

acquire_install_lock() {
  local lock_file="${transaction_state_file}.lock"
  "$node_bin" - "$transaction_state_file" "$lock_file" <<'NODE'
const fs = require("fs");
const path = require("path");

const stateFile = process.argv[2];
const lockFile = process.argv[3];
const uid = typeof process.getuid === "function" ? process.getuid() : 0;

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

if (!path.isAbsolute(stateFile) || !path.isAbsolute(lockFile)) {
  fail("transaction state and lock paths must be absolute");
}
const parent = path.dirname(lockFile);
const segments = path.resolve(parent).split(path.sep).filter(Boolean);
let cursor = path.parse(parent).root;
for (const segment of segments) {
  cursor = path.join(cursor, segment);
  const stat = fs.lstatSync(cursor);
  if (stat.isSymbolicLink() || !stat.isDirectory()) {
    fail("transaction lock parent path is invalid");
  }
  if (stat.uid !== 0 && stat.uid !== uid) {
    fail("transaction lock parent path has an untrusted owner");
  }
  if ((stat.mode & 0o022) !== 0) {
    fail("transaction lock parent path is group/other writable");
  }
}
for (const [pathname, label] of [
  [stateFile, "transaction state file"],
  [lockFile, "transaction lock file"],
]) {
  try {
    const stat = fs.lstatSync(pathname);
    if (stat.isSymbolicLink() || !stat.isFile()) {
      fail(`${label} must be a real regular file`);
    }
    if (stat.uid !== 0 && stat.uid !== uid) {
      fail(`${label} has an untrusted owner`);
    }
    if ((stat.mode & 0o077) !== 0) {
      fail(`${label} must use mode 0600 or stricter`);
    }
  } catch (error) {
    if (error.code !== "ENOENT") {
      throw error;
    }
  }
}
NODE

  umask 077
  exec 9>>"$lock_file"
  chmod 0600 "$lock_file"
  if ! "$flock_bin" -n 9; then
    die "another installation is already running"
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
  env -i \
    "PATH=$TRUSTED_EXEC_PATH" \
    "${parsed_environment[@]}" \
    "$@"
}

run_renderer() {
  local environment_file="$1"
  local renderer="$2"
  local template="$3"
  local output="$4"
  local backend="$5"
  local auth_file="$6"
  load_environment_arguments "$environment_file"
  env -i \
    "PATH=$TRUSTED_EXEC_PATH" \
    "${parsed_environment[@]}" \
    "KBASE_BACKEND_ADDR=$backend" \
    "KBASE_BASIC_AUTH_FILE=$auth_file" \
    "$renderer" \
    "$template" \
    "$output"
}

required_environment_value() {
  local environment_file="$1"
  local wanted_key="$2"
  local entry
  load_environment_arguments "$environment_file"
  for entry in "${parsed_environment[@]}"; do
    if [[ "${entry%%=*}" == "$wanted_key" ]]; then
      printf '%s\n' "${entry#*=}"
      return 0
    fi
  done
  die "candidate environment is missing ${wanted_key}"
}

prepared_manifest_revision() {
  local manifest="$1"
  "$node_bin" - "$manifest" <<'NODE'
const fs = require("fs");
const manifest = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (
  typeof manifest.revision !== "string" ||
  !/^[0-9a-f]{40}$/.test(manifest.revision)
) {
  process.stderr.write("install-release: prepared revision is invalid\n");
  process.exit(1);
}
process.stdout.write(manifest.revision);
NODE
}

validate_public_health_endpoint() {
  local public_origin="$1"
  local endpoint="$2"
  "$node_bin" - "$public_origin" "$endpoint" <<'NODE'
const originValue = process.argv[2];
const endpointValue = process.argv[3];

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

let origin;
let endpoint;
try {
  origin = new URL(originValue);
  endpoint = new URL(endpointValue);
} catch {
  fail("public origin or public health URL is invalid");
}
for (const [value, label] of [
  [origin, "public origin"],
  [endpoint, "public health URL"],
]) {
  if (
    (value.protocol !== "http:" && value.protocol !== "https:") ||
    value.username !== "" ||
    value.password !== "" ||
    value.search !== "" ||
    value.hash !== ""
  ) {
    fail(`${label} must be HTTP(S) without credentials, query, or fragment`);
  }
}
if (origin.pathname !== "/" || endpoint.pathname !== "/health") {
  fail("public origin must be site-root and public health URL must end at /health");
}
const expected = new URL("/health", origin).toString();
if (endpoint.toString() !== expected) {
  fail("public health URL must match KBASE_PUBLIC_ORIGIN/health");
}
NODE
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

read_health_revision() {
  local url="$1"
  local allow_legacy="${2:-0}"
  local response_file="${release_staging}/health-response.json"
  if ! "$curl_bin" \
    -q \
	    --noproxy '*' \
	    --proto '=http,https' \
	    --header 'Cache-Control: no-cache' \
	    --fail \
    --silent \
    --show-error \
    --max-time 2 \
    "$url" >"$response_file"; then
    rm -f "$response_file"
    return 1
  fi
  if ! "$node_bin" - "$response_file" "$allow_legacy" <<'NODE'
const fs = require("fs");
const allowLegacy = process.argv[3] === "1";
let value;
try {
  value = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
} catch {
  process.exit(1);
}
if (
  allowLegacy &&
  value !== null &&
  !Array.isArray(value) &&
  typeof value === "object" &&
  JSON.stringify(Object.keys(value).sort()) ===
    JSON.stringify(["ok", "service"]) &&
  value.ok === true &&
  value.service === "dedao-kbase"
) {
  process.stdout.write("legacy");
  process.exit(0);
}
if (
  value === null ||
  Array.isArray(value) ||
  typeof value !== "object" ||
  JSON.stringify(Object.keys(value).sort()) !==
    JSON.stringify(["ok", "revision", "service"]) ||
  value.ok !== true ||
  value.service !== "dedao-kbase" ||
  typeof value.revision !== "string" ||
  value.revision.length === 0
) {
  process.exit(1);
}
process.stdout.write(value.revision);
NODE
  then
    rm -f "$response_file"
    return 1
  fi
  rm -f "$response_file"
}

retry_health() {
  local attempts="$1"
  local url="$2"
  local expected_revision="$3"
  local index=1
  local actual_revision
  local allow_legacy
  while ((index <= attempts)); do
    allow_legacy=0
    [[ "$expected_revision" == "legacy" ]] && allow_legacy=1
    if actual_revision="$(read_health_revision "$url" "$allow_legacy")" &&
      [[ "$actual_revision" == "$expected_revision" ]]; then
      return 0
    fi
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

write_transaction_journal() {
  "$node_bin" - \
    "$transaction_state_file" \
    "$backup_dir" \
    "$binary_target" \
    "$web_target" \
    "$env_target" \
    "$nginx_config_target" \
    "$service_name" \
    "$nginx_service_name" \
    "$health_url" \
    "$public_health_url" \
    "$previous_revision" \
    "$web_displaced" <<'NODE'
const fs = require("fs");
const path = require("path");

const [
  stateFile,
  backupDirectory,
  binaryTarget,
  webTarget,
  environmentTarget,
  nginxTarget,
  serviceName,
  nginxServiceName,
  backendHealthUrl,
  publicHealthUrl,
  previousRevision,
  webDisplaced,
] = process.argv.slice(2);
const parent = path.dirname(stateFile);
const temporary = path.join(
  parent,
  `.${path.basename(stateFile)}.staging.${process.pid}`,
);
const journal = {
  schema_version: 1,
  backup_dir: backupDirectory,
  targets: {
    binary: binaryTarget,
    web: webTarget,
    environment: environmentTarget,
    nginx: nginxTarget,
  },
  services: {
    kbase: serviceName,
    nginx: nginxServiceName,
  },
  health: {
    backend: backendHealthUrl,
    public: publicHealthUrl,
    previous_revision: previousRevision,
  },
  web_displaced: webDisplaced,
};
let descriptor;
try {
  descriptor = fs.openSync(
    temporary,
    fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_WRONLY,
    0o600,
  );
  fs.writeFileSync(descriptor, `${JSON.stringify(journal, null, 2)}\n`);
  fs.fsyncSync(descriptor);
  fs.closeSync(descriptor);
  descriptor = undefined;
  fs.linkSync(temporary, stateFile);
  fs.unlinkSync(temporary);
  const parentDescriptor = fs.openSync(parent, fs.constants.O_RDONLY);
  fs.fsyncSync(parentDescriptor);
  fs.closeSync(parentDescriptor);
} catch (error) {
  if (descriptor !== undefined) {
    fs.closeSync(descriptor);
  }
  try {
    fs.unlinkSync(temporary);
  } catch {}
  process.stderr.write(
    `install-release: cannot persist transaction journal: ${error.message}\n`,
  );
  process.exit(1);
}
NODE
}

clear_transaction_journal() {
  [[ -e "$transaction_state_file" || -L "$transaction_state_file" ]] || return 0
  "$node_bin" - "$transaction_state_file" <<'NODE'
const fs = require("fs");
const path = require("path");
const stateFile = process.argv[2];
const parent = path.dirname(stateFile);
fs.unlinkSync(stateFile);
const descriptor = fs.openSync(parent, fs.constants.O_RDONLY);
fs.fsyncSync(descriptor);
fs.closeSync(descriptor);
NODE
}

recover_unfinished_transaction() {
  [[ -e "$transaction_state_file" || -L "$transaction_state_file" ]] ||
    return 0

  local requested_backup_dir="$backup_dir"
  local recovered_values
  local recovered_backup=""
  local recovered_previous_revision=""
  local recovered_web_displaced=""
  recovered_values="$(
    mktemp "$(parent_directory "$transaction_state_file")/.kbase-recovery.XXXXXX"
  )"
  chmod 0600 "$recovered_values"
  if ! "$node_bin" - \
    "$transaction_state_file" \
    "$binary_target" \
    "$web_target" \
    "$env_target" \
    "$nginx_config_target" \
    "$service_name" \
    "$nginx_service_name" \
    "$health_url" \
    "$public_health_url" \
    >"$recovered_values" <<'NODE'
const fs = require("fs");
const path = require("path");

const [
  stateFile,
  binaryTarget,
  webTarget,
  environmentTarget,
  nginxTarget,
  serviceName,
  nginxServiceName,
  backendHealthUrl,
  publicHealthUrl,
] = process.argv.slice(2);

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

function exactKeys(value, keys, label) {
  if (value === null || Array.isArray(value) || typeof value !== "object") {
    fail(`${label} must be an object`);
  }
  if (
    JSON.stringify(Object.keys(value).sort()) !==
    JSON.stringify([...keys].sort())
  ) {
    fail(`${label} has unexpected fields`);
  }
}

let journal;
try {
  journal = JSON.parse(fs.readFileSync(stateFile, "utf8"));
} catch (error) {
  fail(`transaction journal is invalid: ${error.message}`);
}
exactKeys(
  journal,
  [
    "schema_version",
    "backup_dir",
    "targets",
    "services",
    "health",
    "web_displaced",
  ],
  "transaction journal",
);
exactKeys(journal.targets, ["binary", "web", "environment", "nginx"], "targets");
exactKeys(journal.services, ["kbase", "nginx"], "services");
exactKeys(
  journal.health,
  ["backend", "public", "previous_revision"],
  "health",
);
if (journal.schema_version !== 1) {
  fail("transaction journal schema is unsupported");
}
const expectedTargets = {
  binary: binaryTarget,
  web: webTarget,
  environment: environmentTarget,
  nginx: nginxTarget,
};
const expectedServices = { kbase: serviceName, nginx: nginxServiceName };
if (JSON.stringify(journal.targets) !== JSON.stringify(expectedTargets)) {
  fail("transaction journal targets do not match this installation");
}
if (JSON.stringify(journal.services) !== JSON.stringify(expectedServices)) {
  fail("transaction journal services do not match this installation");
}
if (
  journal.health.backend !== backendHealthUrl ||
  journal.health.public !== publicHealthUrl
) {
  fail("transaction journal health endpoints do not match this installation");
}
if (
  typeof journal.health.previous_revision !== "string" ||
  journal.health.previous_revision.length === 0
) {
  fail("transaction journal previous revision is invalid");
}
for (const [value, label] of [
  [journal.backup_dir, "backup directory"],
  [journal.web_displaced, "displaced Web path"],
]) {
  if (typeof value !== "string" || !path.isAbsolute(value)) {
    fail(`${label} in transaction journal is invalid`);
  }
}

const currentUid = typeof process.getuid === "function" ? process.getuid() : 0;
function trustedExistingDirectory(pathname, label) {
  const normalized = path.resolve(pathname);
  const segments = normalized.split(path.sep).filter(Boolean);
  let cursor = path.parse(normalized).root;
  for (let index = 0; index < segments.length; index += 1) {
    cursor = path.join(cursor, segments[index]);
    const stat = fs.lstatSync(cursor);
    if (stat.isSymbolicLink() || !stat.isDirectory()) {
      fail(`${label} path is not a real directory`);
    }
    if (currentUid === 0 ? stat.uid !== 0 : stat.uid !== 0 && stat.uid !== currentUid) {
      fail(`${label} path has an untrusted owner`);
    }
    if ((stat.mode & 0o022) !== 0) {
      const trustedSticky =
        stat.uid === 0 && (stat.mode & 0o1000) !== 0;
      if (!trustedSticky) {
        fail(`${label} path is group/other writable`);
      }
    }
  }
}
trustedExistingDirectory(journal.backup_dir, "recovery backup");
const snapshot = path.join(journal.backup_dir, "snapshot");
trustedExistingDirectory(snapshot, "recovery snapshot");
for (const name of ["kbase-server", "service.env", "nginx.conf"]) {
  const stat = fs.lstatSync(path.join(snapshot, name));
  if (stat.isSymbolicLink() || !stat.isFile()) {
    fail(`recovery snapshot is missing ${name}`);
  }
}
const webStat = fs.lstatSync(path.join(snapshot, "web"));
if (webStat.isSymbolicLink() || !webStat.isDirectory()) {
  fail("recovery snapshot is missing Web content");
}

process.stdout.write(
  `${journal.backup_dir}\0${journal.health.previous_revision}\0${journal.web_displaced}\0`,
);
NODE
  then
    rm -f "$recovered_values"
    return 1
  fi
  exec 3<"$recovered_values"
  IFS= read -r -d '' recovered_backup <&3
  IFS= read -r -d '' recovered_previous_revision <&3
  IFS= read -r -d '' recovered_web_displaced <&3
  exec 3<&-
  rm -f "$recovered_values"

  backup_dir="$recovered_backup"
  previous_revision="$recovered_previous_revision"
  web_displaced="$recovered_web_displaced"
  release_staging="${backup_dir}/recovery-staging"
  rm -rf "$release_staging"
  mkdir -m 0700 "$release_staging"
  transaction_active=1
  rollback_in_progress=0
  printf 'install-release: recovering interrupted installation\n' >&2
  rollback_transaction
  backup_dir="$requested_backup_dir"
  rollback_in_progress=0
  transaction_active=0
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

    if [[ "$failed" == "0" ]] && ! sync_installed_targets; then
      printf 'install-release: rollback could not persist restored targets\n' >&2
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
  if ! retry_health 5 "$health_url" "$previous_revision"; then
    printf 'install-release: rollback health check failed\n' >&2
    failed=1
  fi
  if ! retry_health 5 "$public_health_url" "$previous_revision"; then
    printf 'install-release: rollback public health check failed\n' >&2
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
  if ! clear_transaction_journal; then
    printf 'install-release: rollback could not clear transaction journal\n' >&2
    return 1
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
    "$transaction_state_file" \
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
  transactionStateFile,
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
  ["transaction state file", transactionStateFile],
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
const recovering = fs.existsSync(transactionStateFile);
let webStat;
try {
  webStat = fs.lstatSync(webTarget);
} catch (error) {
  if (!recovering || error.code !== "ENOENT") {
    fail("web target must be a real directory");
  }
}
if (webStat && (webStat.isSymbolicLink() || !webStat.isDirectory())) {
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
      fail(`${label} path is group/other writable`);
    }
  }
}

trustedPath(envSource, "environment source", "file");
trustedPath(basicAuth, "basic auth file", "file");
trustedPath(publicKey, "trusted public key", "file");
trustedPath(path.dirname(backupDirectory), "backup parent", "directory");
trustedPath(
  path.dirname(transactionStateFile),
  "transaction state parent",
  "directory",
);
if (fs.existsSync(transactionStateFile)) {
  const stateStat = regular(transactionStateFile, "transaction state file");
  if ((stateStat.mode & 0o077) !== 0) {
    fail("transaction state file must use mode 0600 or stricter");
  }
  trustedPath(transactionStateFile, "transaction state file", "file");
}
trustedPath(binaryTarget, "binary target", "file");
if (webStat) {
  trustedPath(webTarget, "web target", "directory");
} else {
  trustedPath(path.dirname(webTarget), "web target parent", "directory");
}
trustedPath(envTarget, "environment target", "file");
trustedPath(nginxTarget, "Nginx config target", "file");

const targets = [binaryTarget, webTarget, envTarget, nginxTarget].map((target) =>
  target === webTarget && !webStat
    ? path.join(fs.realpathSync(path.dirname(target)), path.basename(target))
    : fs.realpathSync(target),
);
const transactionStateParent = fs.realpathSync(
  path.dirname(transactionStateFile),
);
const realTransactionStateFile = path.join(
  transactionStateParent,
  path.basename(transactionStateFile),
);
for (const target of targets) {
  if (
    contains(target, realTransactionStateFile) ||
    contains(realTransactionStateFile, target)
  ) {
    fail("transaction state file must not overlap an installation target");
  }
}
if (new Set(targets).size !== targets.length) {
  fail("installation targets must be distinct");
}
const identities = targets
  .filter((target) => fs.existsSync(target))
  .map((target) => {
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
  transaction_state_parent="$(parent_directory "$transaction_state_file")"
  if [[ ! -d "$transaction_state_parent" || ! -w "$transaction_state_parent" ]]; then
    die "transaction state parent must exist and be writable"
  fi
}

validate_web_archive() {
  local archive="$1"
  local validation_dir="$2"
  local members_file="${validation_dir}/web-members.json"

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

  "$node_bin" "$ARCHIVE_LISTER" \
    --archive "$archive" \
    --gzip-bin "$gzip_bin" \
    --tar-bin "$tar_bin" \
    --timeout-ms "$ARCHIVE_LIST_TIMEOUT_MS" \
    --stdout-limit-bytes "$ARCHIVE_LIST_STDOUT_LIMIT_BYTES" \
    --stderr-limit-bytes "$ARCHIVE_LIST_STDERR_LIMIT_BYTES" \
    >"$members_file"

  "$node_bin" - \
    "$members_file" \
    "$WEB_ROOT_NAME" \
    "$MAX_WEB_MEMBERS" \
    "$MAX_WEB_FILE_BYTES" \
    "$MAX_WEB_TOTAL_BYTES" <<'NODE'
const fs = require("fs");

const membersPath = process.argv[2];
const expectedRoot = process.argv[3];
const maxMembers = Number(process.argv[4]);
const maxFileBytes = Number(process.argv[5]);
const maxTotalBytes = Number(process.argv[6]);

function fail(message) {
  process.stderr.write(`install-release: ${message}\n`);
  process.exit(1);
}

const document = JSON.parse(fs.readFileSync(membersPath, "utf8"));
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
if (document.members.length > maxMembers) {
  fail("web archive exceeds the member count limit");
}

let totalBytes = 0;
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
  if (member.type !== "file" && member.type !== "directory") {
    fail(`web archive member type is not allowed: ${name}`);
  }
  const size = member.size;
  if (!Number.isSafeInteger(size) || size < 0) {
    fail(`web archive member has an invalid size: ${name}`);
  }
  if (member.type === "file" && size > maxFileBytes) {
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
  "$node_bin" "$FSYNC_HELPER" \
    --tree "$snapshot" \
    --path "$backup_dir" \
    --path "$(parent_directory "$backup_dir")"
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

  "$node_bin" "$FSYNC_HELPER" \
    --path "$binary_temp" \
    --tree "$web_temp" \
    --path "$env_temp" \
    --path "$nginx_temp" \
    --path "$(parent_directory "$binary_target")" \
    --path "$(parent_directory "$web_target")" \
    --path "$(parent_directory "$env_target")" \
    --path "$(parent_directory "$nginx_config_target")"
}

sync_installed_targets() {
  "$node_bin" "$FSYNC_HELPER" \
    --path "$binary_target" \
    --tree "$web_target" \
    --path "$env_target" \
    --path "$nginx_config_target" \
    --path "$(parent_directory "$binary_target")" \
    --path "$(parent_directory "$web_target")" \
    --path "$(parent_directory "$env_target")" \
    --path "$(parent_directory "$nginx_config_target")"
}

stage_prepared_release() {
  local manifest="$1"
  local env_source="$2"
  local public_key="$3"
  local original_release
  local staged_release

  [[ "$(base_name "$manifest")" == "prepared-manifest.json" ]] ||
    die "prepared manifest must use the canonical filename"
  original_release="$(cd "$(dirname "$manifest")" && pwd -P)"
  staged_release="${release_staging}/release"
  mkdir -m 0700 "$staged_release" "${staged_release}/bundle"
  staged_public_key="${release_staging}/trusted-release-public-key.pem"
  staged_environment="${release_staging}/candidate.env"
  "$node_bin" "$STAGE_HELPER" \
    --file \
    "${original_release}/prepared-manifest.json" \
    "${staged_release}/prepared-manifest.json" \
    "$MAX_PREPARED_MANIFEST_BYTES" \
    0600 \
    --file \
    "${original_release}/MANIFEST.sig" \
    "${staged_release}/MANIFEST.sig" \
    "$MAX_RELEASE_SIGNATURE_BYTES" \
    0600 \
    --file \
    "${original_release}/bundle/kbase-server" \
    "${staged_release}/bundle/kbase-server" \
    "$MAX_SERVER_BINARY_BYTES" \
    0755 \
    --file \
    "${original_release}/bundle/web.tar.gz" \
    "${staged_release}/bundle/web.tar.gz" \
    "$MAX_WEB_ARCHIVE_BYTES" \
    0644 \
    --file \
    "${original_release}/bundle/kbase.locations.conf.template" \
    "${staged_release}/bundle/kbase.locations.conf.template" \
    "$MAX_NGINX_TEMPLATE_BYTES" \
    0644 \
    --file \
    "${original_release}/bundle/render-kbase-config.sh" \
    "${staged_release}/bundle/render-kbase-config.sh" \
    "$MAX_CONFIG_RENDERER_BYTES" \
    0755 \
    --file \
    "$public_key" \
    "$staged_public_key" \
    "$MAX_PUBLIC_KEY_BYTES" \
    0600 \
    --file \
    "$env_source" \
    "$staged_environment" \
    "$MAX_ENVIRONMENT_BYTES" \
    0600
  staged_manifest="${staged_release}/prepared-manifest.json"

  "$PREPARER" verify \
    --node-bin "$node_bin" \
    --tar-bin "$tar_bin" \
    --gzip-bin "$gzip_bin" \
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
  local public_origin
  local previous_public_revision

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
  recover_unfinished_transaction

  (umask 077; mkdir "$backup_dir")
  chmod 0700 "$backup_dir"
  staging="${backup_dir}/staging"
  mkdir -m 0700 "$staging"
  release_staging="$staging"
  stage_prepared_release "$manifest" "$env_source" "$trusted_public_key"
  candidate_revision="$(prepared_manifest_revision "$staged_manifest")"
  public_origin="$(
    required_environment_value "$staged_environment" KBASE_PUBLIC_ORIGIN
  )"
  validate_public_health_endpoint "$public_origin" "$public_health_url"
  previous_revision="$(read_health_revision "$health_url" 1)" ||
    die "current backend health contract is unavailable"
  previous_public_revision="$(read_health_revision "$public_health_url" 1)" ||
    die "current public health contract is unavailable"
  if [[ "$previous_public_revision" != "$previous_revision" ]]; then
    die "backend and public health revisions disagree before installation"
  fi

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

  write_transaction_journal
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
  sync_installed_targets

  "$systemctl_bin" restart "$service_name"
  retry_health 20 "$health_url" "$candidate_revision"
  "$nginx_bin" -t
  "$systemctl_bin" reload "$nginx_service_name"
  retry_health 20 "$public_health_url" "$candidate_revision"

  rm -rf "$web_displaced"
  web_displaced=""
  rm -rf "$staging"
  release_staging=""
  clear_transaction_journal
  transaction_active=0
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
      --public-health-url)
        require_value "$1" "${2:-}"
        public_health_url="$2"
        shift 2
        ;;
      --backup-dir)
        require_value "$1" "${2:-}"
        backup_dir="$2"
        shift 2
        ;;
      --transaction-state-file)
        require_value "$1" "${2:-}"
        transaction_state_file="$2"
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
      --flock-bin)
        require_value "$1" "${2:-}"
        flock_bin="$2"
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
    "$public_health_url" \
    "$backup_dir" \
    "$transaction_state_file"; do
    if [[ -z "$value" ]]; then
      usage >&2
      die "all install options are required"
    fi
  done

  validate_privileged_runtime
  if [[ ! -x "$PREPARER" ]]; then
    die "prepare-release.sh is missing or not executable"
  fi
  if [[ ! -x "$SIGNATURE_HELPER" ]]; then
    die "release-signature.sh is missing or not executable"
  fi
  if [[ ! -f "$ARCHIVE_LISTER" || -L "$ARCHIVE_LISTER" ]]; then
    die "archive listing helper is missing"
  fi
  if [[ ! -f "$FSYNC_HELPER" || -L "$FSYNC_HELPER" ]]; then
    die "filesystem sync helper is missing"
  fi
  if [[ ! -f "$STAGE_HELPER" || -L "$STAGE_HELPER" ]]; then
    die "bounded staging helper is missing"
  fi
  require_executable "$node_bin" "Node"
  require_executable "$openssl_bin" "OpenSSL"
  require_executable "$tar_bin" "tar"
  require_executable "$gzip_bin" "gzip"
  require_executable "$nginx_bin" "Nginx"
  require_executable "$systemctl_bin" "systemctl"
  require_executable "$curl_bin" "curl"
  require_executable "$flock_bin" "flock"
  require_executable "$uname_bin" "uname"

  acquire_install_lock
  install_release "$manifest" "$env_source" "$basic_auth_file" "$backend_addr"
}

main "$@"
