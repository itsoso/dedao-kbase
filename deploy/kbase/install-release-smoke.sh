#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INSTALLER="${ROOT}/deploy/kbase/install-release.sh"
PREPARER="${ROOT}/deploy/kbase/prepare-release.sh"
NODE_BIN="$(command -v node)"
REAL_TAR="$(command -v tar)"
REAL_GZIP="$(command -v gzip)"
TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kbase-install-release.XXXXXX")"

cleanup() {
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

fail() {
  printf 'install release smoke: FAIL: %s\n' "$*" >&2
  exit 1
}

mode_of() {
  "$NODE_BIN" - "$1" <<'NODE'
const fs = require("fs");
process.stdout.write((fs.statSync(process.argv[2]).mode & 0o777).toString(8).padStart(4, "0"));
NODE
}

write_prepared_manifest() {
  local release_dir="$1"
  "$NODE_BIN" - "$release_dir" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const releaseDirectory = process.argv[2];
const specifications = [
  ["kbase-server", "bundle/kbase-server", "0755"],
  ["web", "bundle/web.tar.gz", "0644"],
  ["nginx-template", "bundle/kbase.locations.conf.template", "0644"],
  ["config-renderer", "bundle/render-kbase-config.sh", "0755"],
];
const artifacts = specifications.map(([name, relativePath, mode]) => {
  const value = fs.readFileSync(path.join(releaseDirectory, relativePath));
  return {
    name,
    path: relativePath,
    sha256: crypto.createHash("sha256").update(value).digest("hex"),
    mode,
  };
});
const manifest = {
  schema: "dedao-kbase-prepared-release/v1",
  revision: "1234567890abcdef1234567890abcdef12345678",
  artifacts,
};
fs.writeFileSync(
  path.join(releaseDirectory, "prepared-manifest.json"),
  `${JSON.stringify(manifest, null, 2)}\n`,
  { mode: 0o644 },
);
NODE
}

create_release() {
  local release_dir="$1"
  local web_source="${release_dir}/web-source"
  mkdir -p "${release_dir}/bundle" "${web_source}/frontend-web"

  printf 'NEW_WEB\n' >"${web_source}/frontend-web/index.html"
  "$REAL_TAR" -C "$web_source" -cf - frontend-web |
    "$REAL_GZIP" -n >"${release_dir}/bundle/web.tar.gz"

  cat >"${release_dir}/bundle/kbase-server" <<'CANDIDATE'
#!/usr/bin/env bash
set -Eeuo pipefail
# CANDIDATE_BINARY_MARKER
printf 'doctor\n' >>"$INSTALL_LOG"
[[ "${1:-}" == "--check-config" ]]
[[ "${2:-}" == "--web-dir" ]]
[[ -f "${3:-}/index.html" ]]
grep -q '^OLD_BINARY_MARKER$' "$ASSERT_BINARY_TARGET"
grep -q '^OLD_WEB$' "$ASSERT_WEB_TARGET/index.html"
grep -q '^OLD_ENV=' "$ASSERT_ENV_TARGET"
grep -q '^OLD_NGINX$' "$ASSERT_NGINX_TARGET"
[[ -n "${KBASE_BROWSER_SESSION_SECRET:-}" ]]
if [[ "${DOCTOR_FAIL:-0}" == "1" ]]; then
  printf 'doctor rejected fixture\n' >&2
  exit 42
fi
printf '{"schema_version":1,"status":"ok"}\n'
CANDIDATE
  chmod 0755 "${release_dir}/bundle/kbase-server"

  cat >"${release_dir}/bundle/kbase.locations.conf.template" <<'TEMPLATE'
location / {
  proxy_pass http://__KBASE_BACKEND_ADDR__;
}
TEMPLATE
  chmod 0644 "${release_dir}/bundle/kbase.locations.conf.template"

  cat >"${release_dir}/bundle/render-kbase-config.sh" <<'RENDERER'
#!/usr/bin/env bash
set -Eeuo pipefail
template="${1:-}"
output="${2:-}"
[[ -f "$template" ]]
[[ -n "$output" ]]
[[ "${RENDER_SECRET:-}" == "available" ]]
[[ -n "${KBASE_BROWSER_SESSION_SECRET:-}" ]]
[[ "${KBASE_BACKEND_ADDR:-}" == "127.0.0.1:8719" ]]
[[ "${KBASE_BASIC_AUTH_FILE:-}" == "$ASSERT_BASIC_AUTH_FILE" ]]
grep -q '^OLD_BINARY_MARKER$' "$ASSERT_BINARY_TARGET"
grep -q '^OLD_WEB$' "$ASSERT_WEB_TARGET/index.html"
grep -q '^OLD_ENV=' "$ASSERT_ENV_TARGET"
grep -q '^OLD_NGINX$' "$ASSERT_NGINX_TARGET"
printf 'render\n' >>"$INSTALL_LOG"
printf 'RENDERED_NGINX\nbackend=%s\nauth=%s\n' \
  "$KBASE_BACKEND_ADDR" \
  "$KBASE_BASIC_AUTH_FILE" >"$output"
RENDERER
  chmod 0755 "${release_dir}/bundle/render-kbase-config.sh"

  write_prepared_manifest "$release_dir"
  rm -rf "$web_source"
  "$PREPARER" verify \
    --node-bin "$NODE_BIN" \
    --manifest "${release_dir}/prepared-manifest.json"
}

setup_fake_tools() {
  local fake_dir="$1"
  mkdir -p "$fake_dir"

  cat >"${fake_dir}/uname" <<'FAKE_UNAME'
#!/usr/bin/env bash
set -Eeuo pipefail
[[ "${1:-}" == "-s" ]]
printf 'Linux\n'
FAKE_UNAME

  cat >"${fake_dir}/systemctl" <<'FAKE_SYSTEMCTL'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'systemctl:%s:%s\n' "${1:-}" "${2:-}" >>"$INSTALL_LOG"
FAKE_SYSTEMCTL

  cat >"${fake_dir}/nginx" <<'FAKE_NGINX'
#!/usr/bin/env bash
set -Eeuo pipefail
[[ "${1:-}" == "-t" ]]
printf 'nginx:-t\n' >>"$INSTALL_LOG"
FAKE_NGINX

  cat >"${fake_dir}/curl" <<'FAKE_CURL'
#!/usr/bin/env bash
set -Eeuo pipefail
if grep -q 'CANDIDATE_BINARY_MARKER' "$ASSERT_BINARY_TARGET"; then
  printf 'health:candidate\n' >>"$INSTALL_LOG"
  if [[ "${FAIL_HEALTH:-0}" == "1" ]]; then
    if [[ "${FAIL_ROLLBACK_PREP:-0}" == "1" ]]; then
      rm -rf "${BACKUP_DIR}/snapshot/web"
    fi
    exit 22
  fi
else
  printf 'health:old\n' >>"$INSTALL_LOG"
fi
printf '{"status":"ok"}\n'
FAKE_CURL

  cat >"${fake_dir}/tar" <<'FAKE_TAR'
#!/usr/bin/env bash
set -Eeuo pipefail
arguments=()
for argument in "$@"; do
  case "$argument" in
    --quoting-style=escape) ;;
    *) arguments+=("$argument") ;;
  esac
done
exec "$REAL_TAR" "${arguments[@]}"
FAKE_TAR

  chmod 0755 \
    "${fake_dir}/uname" \
    "${fake_dir}/systemctl" \
    "${fake_dir}/nginx" \
    "${fake_dir}/curl" \
    "${fake_dir}/tar"
}

setup_case() {
  local name="$1"
  CASE_DIR="${TMP_ROOT}/${name}"
  TARGET_ROOT="${CASE_DIR}/targets"
  BINARY_TARGET="${TARGET_ROOT}/bin/kbase-server"
  WEB_TARGET="${TARGET_ROOT}/web"
  ENV_SOURCE="${CASE_DIR}/candidate.env"
  ENV_TARGET="${TARGET_ROOT}/service.env"
  NGINX_TARGET="${TARGET_ROOT}/nginx/kbase.locations.conf"
  BASIC_AUTH_FILE="${CASE_DIR}/browser.htpasswd"
  BACKUP_DIR="${CASE_DIR}/backups/release-1"
  INSTALL_LOG="${CASE_DIR}/install.log"

  mkdir -p \
    "${TARGET_ROOT}/bin" \
    "$WEB_TARGET" \
    "${TARGET_ROOT}/nginx" \
    "$(dirname "$BACKUP_DIR")"

  cat >"$BINARY_TARGET" <<'OLD_BINARY'
#!/usr/bin/env bash
OLD_BINARY_MARKER
OLD_BINARY
  chmod 0700 "$BINARY_TARGET"
  printf 'OLD_WEB\n' >"${WEB_TARGET}/index.html"
  chmod 0750 "$WEB_TARGET"
  chmod 0640 "${WEB_TARGET}/index.html"
  printf 'OLD_ENV=value\n' >"$ENV_TARGET"
  chmod 0640 "$ENV_TARGET"
  printf 'OLD_NGINX\n' >"$NGINX_TARGET"
  chmod 0644 "$NGINX_TARGET"
  printf 'user:fixture\n' >"$BASIC_AUTH_FILE"
  chmod 0600 "$BASIC_AUTH_FILE"
  cat >"$ENV_SOURCE" <<'ENVIRONMENT'
KBASE_BROWSER_SESSION_SECRET=test_only_browser_session_value_1234567890
RENDER_SECRET=available
ENVIRONMENT
  chmod 0600 "$ENV_SOURCE"
  : >"$INSTALL_LOG"

  export CASE_DIR TARGET_ROOT BINARY_TARGET WEB_TARGET
  export ENV_SOURCE ENV_TARGET NGINX_TARGET BASIC_AUTH_FILE
  export BACKUP_DIR INSTALL_LOG
  export ASSERT_BINARY_TARGET="$BINARY_TARGET"
  export ASSERT_WEB_TARGET="$WEB_TARGET"
  export ASSERT_ENV_TARGET="$ENV_TARGET"
  export ASSERT_NGINX_TARGET="$NGINX_TARGET"
  export ASSERT_BASIC_AUTH_FILE="$BASIC_AUTH_FILE"
  export REAL_TAR
  unset FAIL_HEALTH FAIL_ROLLBACK_PREP
}

save_expected_targets() {
  EXPECTED_ROOT="${CASE_DIR}/expected"
  mkdir -p "$EXPECTED_ROOT"
  cp -p "$BINARY_TARGET" "${EXPECTED_ROOT}/kbase-server"
  cp -a "$WEB_TARGET" "${EXPECTED_ROOT}/web"
  cp -p "$ENV_TARGET" "${EXPECTED_ROOT}/service.env"
  cp -p "$NGINX_TARGET" "${EXPECTED_ROOT}/nginx.conf"
  BINARY_MODE_BEFORE="$(mode_of "$BINARY_TARGET")"
  WEB_MODE_BEFORE="$(mode_of "$WEB_TARGET")"
  ENV_MODE_BEFORE="$(mode_of "$ENV_TARGET")"
  NGINX_MODE_BEFORE="$(mode_of "$NGINX_TARGET")"
}

assert_targets_match_expected() {
  cmp "$BINARY_TARGET" "${EXPECTED_ROOT}/kbase-server" ||
    fail "binary target was not restored"
  diff -r "$WEB_TARGET" "${EXPECTED_ROOT}/web" >/dev/null ||
    fail "web target was not restored"
  cmp "$ENV_TARGET" "${EXPECTED_ROOT}/service.env" ||
    fail "environment target was not restored"
  cmp "$NGINX_TARGET" "${EXPECTED_ROOT}/nginx.conf" ||
    fail "Nginx target was not restored"
  [[ "$(mode_of "$BINARY_TARGET")" == "$BINARY_MODE_BEFORE" ]] ||
    fail "binary mode was not restored"
  [[ "$(mode_of "$WEB_TARGET")" == "$WEB_MODE_BEFORE" ]] ||
    fail "web directory mode was not restored"
  [[ "$(mode_of "$ENV_TARGET")" == "$ENV_MODE_BEFORE" ]] ||
    fail "environment mode was not restored"
  [[ "$(mode_of "$NGINX_TARGET")" == "$NGINX_MODE_BEFORE" ]] ||
    fail "Nginx mode was not restored"
}

assert_no_target_temporary_entries() {
  local found
  found="$(
    find "$TARGET_ROOT" \
      -maxdepth 3 \
      -name '.*.kbase-install.*' \
      -print \
      -quit
  )"
  [[ -z "$found" ]] || fail "temporary target sibling was left behind"
}

run_install() {
  local manifest="$1"
  "$INSTALLER" install \
    --manifest "$manifest" \
    --binary-target "$BINARY_TARGET" \
    --web-target "$WEB_TARGET" \
    --env-source "$ENV_SOURCE" \
    --env-target "$ENV_TARGET" \
    --nginx-config-target "$NGINX_TARGET" \
    --basic-auth-file "$BASIC_AUTH_FILE" \
    --backend-addr 127.0.0.1:8719 \
    --service-name dedao-kbase.service \
    --nginx-service-name nginx.service \
    --health-url http://127.0.0.1:8719/health \
    --backup-dir "$BACKUP_DIR" \
    --node-bin "$NODE_BIN" \
    --tar-bin "${FAKE_TOOLS}/tar" \
    --gzip-bin "$REAL_GZIP" \
    --nginx-bin "${FAKE_TOOLS}/nginx" \
    --systemctl-bin "${FAKE_TOOLS}/systemctl" \
    --curl-bin "${FAKE_TOOLS}/curl" \
    --uname-bin "${FAKE_TOOLS}/uname"
}

assert_log_order() {
  "$NODE_BIN" - "$INSTALL_LOG" <<'NODE'
const fs = require("fs");
const lines = fs.readFileSync(process.argv[2], "utf8").trim().split("\n");
const expected = [
  "doctor",
  "render",
  "systemctl:restart:dedao-kbase.service",
  "health:candidate",
  "nginx:-t",
  "systemctl:reload:nginx.service",
  "health:candidate",
];
let cursor = -1;
for (const value of expected) {
  const next = lines.indexOf(value, cursor + 1);
  if (next < 0) {
    throw new Error(`missing or out-of-order install event: ${value}`);
  }
  cursor = next;
}
NODE
}

assert_rollback_log() {
  "$NODE_BIN" - "$INSTALL_LOG" <<'NODE'
const fs = require("fs");
const lines = fs.readFileSync(process.argv[2], "utf8").trim().split("\n");
const firstRestart = lines.indexOf("systemctl:restart:dedao-kbase.service");
const firstCandidateHealth = lines.indexOf("health:candidate", firstRestart + 1);
const rollbackRestart = lines.indexOf(
  "systemctl:restart:dedao-kbase.service",
  firstCandidateHealth + 1,
);
const nginxTest = lines.indexOf("nginx:-t", rollbackRestart + 1);
const nginxReload = lines.indexOf(
  "systemctl:reload:nginx.service",
  nginxTest + 1,
);
const oldHealth = lines.indexOf("health:old", nginxReload + 1);
if (
  firstRestart < 0 ||
  firstCandidateHealth < 0 ||
  rollbackRestart < 0 ||
  nginxTest < 0 ||
  nginxReload < 0 ||
  oldHealth < 0
) {
  throw new Error("rollback service verification sequence is incomplete");
}
NODE
}

assert_pre_mutation_failure() {
  [[ ! -s "$INSTALL_LOG" ]] ||
    fail "pre-mutation rejection invoked doctor or service tools"
  assert_targets_match_expected
  assert_no_target_temporary_entries
}

[[ -x "$INSTALLER" ]] || fail "install-release.sh is missing or not executable"
[[ -x "$PREPARER" ]] || fail "prepare-release.sh is missing or not executable"

FAKE_TOOLS="${TMP_ROOT}/fake-tools"
BASE_RELEASE="${TMP_ROOT}/prepared-release"
setup_fake_tools "$FAKE_TOOLS"
create_release "$BASE_RELEASE"

setup_case success
save_expected_targets
SUCCESS_OUTPUT="${CASE_DIR}/output.json"
run_install "${BASE_RELEASE}/prepared-manifest.json" >"$SUCCESS_OUTPUT"
grep -q '"status":"installed"' "$SUCCESS_OUTPUT" ||
  fail "successful install did not return structured status"
grep -q 'CANDIDATE_BINARY_MARKER' "$BINARY_TARGET" ||
  fail "candidate binary was not installed"
grep -q '^NEW_WEB$' "${WEB_TARGET}/index.html" ||
  fail "candidate web directory was not installed"
cmp "$ENV_SOURCE" "$ENV_TARGET" ||
  fail "candidate environment was not installed"
grep -q '^RENDERED_NGINX$' "$NGINX_TARGET" ||
  fail "rendered Nginx config was not installed"
[[ "$(mode_of "$BINARY_TARGET")" == "0755" ]] ||
  fail "installed binary mode is not 0755"
[[ "$(mode_of "$ENV_TARGET")" == "0600" ]] ||
  fail "installed environment mode is not 0600"
[[ "$(mode_of "$NGINX_TARGET")" == "0600" ]] ||
  fail "installed Nginx mode is not 0600"
[[ "$(mode_of "$BACKUP_DIR")" == "0700" ]] ||
  fail "backup directory mode is not 0700"
cmp "${BACKUP_DIR}/snapshot/kbase-server" "${EXPECTED_ROOT}/kbase-server" ||
  fail "binary snapshot was not retained"
diff -r "${BACKUP_DIR}/snapshot/web" "${EXPECTED_ROOT}/web" >/dev/null ||
  fail "web snapshot was not retained"
cmp "${BACKUP_DIR}/snapshot/service.env" "${EXPECTED_ROOT}/service.env" ||
  fail "environment snapshot was not retained"
cmp "${BACKUP_DIR}/snapshot/nginx.conf" "${EXPECTED_ROOT}/nginx.conf" ||
  fail "Nginx snapshot was not retained"
[[ "$(mode_of "${BACKUP_DIR}/snapshot/kbase-server")" == "$BINARY_MODE_BEFORE" ]] ||
  fail "binary snapshot mode changed"
[[ "$(mode_of "${BACKUP_DIR}/snapshot/web")" == "$WEB_MODE_BEFORE" ]] ||
  fail "web snapshot mode changed"
[[ "$(mode_of "${BACKUP_DIR}/snapshot/service.env")" == "$ENV_MODE_BEFORE" ]] ||
  fail "environment snapshot mode changed"
[[ "$(mode_of "${BACKUP_DIR}/snapshot/nginx.conf")" == "$NGINX_MODE_BEFORE" ]] ||
  fail "Nginx snapshot mode changed"
assert_log_order
assert_no_target_temporary_entries
if grep -q 'test_only_browser_session_value' "$INSTALL_LOG" "$NGINX_TARGET"; then
  fail "installation output leaked an environment secret"
fi

setup_case rollback
save_expected_targets
export FAIL_HEALTH=1
if run_install "${BASE_RELEASE}/prepared-manifest.json" \
  >"${CASE_DIR}/stdout" \
  2>"${CASE_DIR}/stderr"; then
  fail "forced health failure unexpectedly succeeded"
fi
assert_targets_match_expected
assert_rollback_log
[[ -d "$BACKUP_DIR" ]] || fail "rollback removed the backup directory"
assert_no_target_temporary_entries
unset FAIL_HEALTH

setup_case rollback-preparation-failure
save_expected_targets
export FAIL_HEALTH=1
export FAIL_ROLLBACK_PREP=1
if run_install "${BASE_RELEASE}/prepared-manifest.json" \
  >"${CASE_DIR}/stdout" \
  2>"${CASE_DIR}/stderr"; then
  fail "forced rollback preparation failure unexpectedly succeeded"
fi
DISPLACED_WEB="$(
  find "$(dirname "$WEB_TARGET")" \
    -maxdepth 1 \
    -type d \
    -name ".$(basename "$WEB_TARGET").kbase-install.previous.*" \
    -print \
    -quit
)"
[[ -n "$DISPLACED_WEB" ]] ||
  fail "rollback failure deleted the displaced old Web directory"
grep -q '^OLD_WEB$' "${DISPLACED_WEB}/index.html" ||
  fail "displaced old Web directory was not preserved intact"
[[ -d "$BACKUP_DIR" ]] ||
  fail "rollback preparation failure removed the backup directory"
unset FAIL_HEALTH FAIL_ROLLBACK_PREP

setup_case invalid-manifest
save_expected_targets
INVALID_RELEASE="${CASE_DIR}/release"
cp -R "$BASE_RELEASE" "$INVALID_RELEASE"
"$NODE_BIN" - "${INVALID_RELEASE}/prepared-manifest.json" <<'NODE'
const fs = require("fs");
const pathname = process.argv[2];
const manifest = JSON.parse(fs.readFileSync(pathname, "utf8"));
manifest.schema = "dedao-kbase-prepared-release/v999";
fs.writeFileSync(pathname, `${JSON.stringify(manifest)}\n`);
NODE
if run_install "${INVALID_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "invalid manifest unexpectedly installed"
fi
assert_pre_mutation_failure

setup_case doctor-failure
save_expected_targets
printf 'DOCTOR_FAIL=1\n' >>"$ENV_SOURCE"
if run_install "${BASE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "doctor failure unexpectedly installed"
fi
grep -q '^doctor$' "$INSTALL_LOG" ||
  fail "doctor failure did not invoke the configuration doctor"
if grep -Eq '^(render|systemctl:|health:|nginx:)' "$INSTALL_LOG"; then
  fail "doctor failure progressed past the doctor Gate"
fi
assert_targets_match_expected
assert_no_target_temporary_entries

setup_case writable-env
save_expected_targets
chmod 0666 "$ENV_SOURCE"
if run_install "${BASE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "world-writable environment unexpectedly installed"
fi
assert_pre_mutation_failure

setup_case symlink-target
save_expected_targets
OUTSIDE_BINARY="${CASE_DIR}/outside-binary"
cp -p "$BINARY_TARGET" "$OUTSIDE_BINARY"
rm "$BINARY_TARGET"
ln -s "$OUTSIDE_BINARY" "$BINARY_TARGET"
if run_install "${BASE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "symlink target unexpectedly installed"
fi
[[ ! -s "$INSTALL_LOG" ]] ||
  fail "symlink rejection invoked doctor or service tools"
cmp "$OUTSIDE_BINARY" "${EXPECTED_ROOT}/kbase-server" ||
  fail "symlink rejection modified the outside file"
assert_no_target_temporary_entries

setup_case missing-backup-parent
save_expected_targets
BACKUP_DIR="${CASE_DIR}/missing/parent/release"
export BACKUP_DIR
if run_install "${BASE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "missing backup parent unexpectedly installed"
fi
assert_pre_mutation_failure

setup_case overlapping-backup
mkdir -p "${WEB_TARGET}/backups"
BACKUP_DIR="${WEB_TARGET}/backups/release"
export BACKUP_DIR
save_expected_targets
if run_install "${BASE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "backup directory inside Web target unexpectedly installed"
fi
[[ ! -s "$INSTALL_LOG" ]] ||
  fail "overlapping backup path progressed to doctor or service tools"
assert_targets_match_expected
[[ ! -e "$BACKUP_DIR" ]] ||
  fail "overlapping backup path was created"

setup_case overlapping-targets
printf 'OLD_NESTED_BINARY\n' >"${WEB_TARGET}/nested-kbase-server"
chmod 0700 "${WEB_TARGET}/nested-kbase-server"
BINARY_TARGET="${WEB_TARGET}/nested-kbase-server"
export BINARY_TARGET
export ASSERT_BINARY_TARGET="$BINARY_TARGET"
save_expected_targets
if run_install "${BASE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "nested installation targets unexpectedly installed"
fi
[[ ! -s "$INSTALL_LOG" ]] ||
  fail "nested targets progressed to doctor or service tools"
assert_targets_match_expected

setup_case unsafe-web
save_expected_targets
UNSAFE_RELEASE="${CASE_DIR}/release"
cp -R "$BASE_RELEASE" "$UNSAFE_RELEASE"
UNSAFE_SOURCE="${CASE_DIR}/unsafe-source"
OUTSIDE_WEB="${CASE_DIR}/outside-web"
mkdir -p "${UNSAFE_SOURCE}/frontend-web"
printf 'OUTSIDE_UNCHANGED\n' >"$OUTSIDE_WEB"
ln -s "$OUTSIDE_WEB" "${UNSAFE_SOURCE}/frontend-web/escape"
printf 'NEW_WEB\n' >"${UNSAFE_SOURCE}/frontend-web/index.html"
"$REAL_TAR" -C "$UNSAFE_SOURCE" -cf - frontend-web |
  "$REAL_GZIP" -n >"${UNSAFE_RELEASE}/bundle/web.tar.gz"
write_prepared_manifest "$UNSAFE_RELEASE"
"$PREPARER" verify \
  --node-bin "$NODE_BIN" \
  --manifest "${UNSAFE_RELEASE}/prepared-manifest.json"
if run_install "${UNSAFE_RELEASE}/prepared-manifest.json" >/dev/null 2>&1; then
  fail "unsafe web archive unexpectedly installed"
fi
[[ "$(cat "$OUTSIDE_WEB")" == "OUTSIDE_UNCHANGED" ]] ||
  fail "unsafe archive modified an outside file"
assert_pre_mutation_failure

printf 'install release smoke: PASS\n'
