#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSEMBLER="${SCRIPT_DIR}/assemble-release.sh"
PREPARER="${SCRIPT_DIR}/prepare-release.sh"
SIGNATURE_HELPER="${SCRIPT_DIR}/release-signature.sh"
SCHEMA="dedao-kbase-source-release/v1"
PREPARED_SCHEMA="dedao-kbase-prepared-release/v1"
ARCHIVE_PREFIX="dedao-kbase-source/"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "${description}: command unexpectedly succeeded"
  fi
}

write_manifest_variant() {
  source_manifest="$1"
  target_manifest="$2"
  mutation="$3"
  node - "$source_manifest" "$target_manifest" "$mutation" <<'NODE'
const fs = require("fs");

const sourcePath = process.argv[2];
const targetPath = process.argv[3];
const mutation = process.argv[4];
const manifest = JSON.parse(fs.readFileSync(sourcePath, "utf8"));

switch (mutation) {
  case "wrong-schema":
    manifest.schema = "dedao-kbase-source-release/v2";
    break;
  case "bad-revision":
    manifest.revision = "not-a-commit";
    break;
  case "bad-digest":
    manifest.sha256 = "not-a-digest";
    break;
  case "absolute-artifact":
    manifest.artifact = "/tmp/source.tar.gz";
    break;
  case "traversal-artifact":
    manifest.artifact = "../source.tar.gz";
    break;
  case "missing-artifact":
    manifest.artifact = "missing.tar.gz";
    break;
  case "unknown-field":
    manifest.created_at = "not-allowed";
    break;
  default:
    throw new Error(`unknown mutation: ${mutation}`);
}

fs.writeFileSync(targetPath, `${JSON.stringify(manifest, null, 2)}\n`);
NODE
}

write_prepared_manifest_variant() {
  source_manifest="$1"
  target_manifest="$2"
  mutation="$3"
  node - "$source_manifest" "$target_manifest" "$mutation" <<'NODE'
const fs = require("fs");

const sourcePath = process.argv[2];
const targetPath = process.argv[3];
const mutation = process.argv[4];
const manifest = JSON.parse(fs.readFileSync(sourcePath, "utf8"));

switch (mutation) {
  case "wrong-schema":
    manifest.schema = "dedao-kbase-prepared-release/v2";
    break;
  case "bad-revision":
    manifest.revision = "not-a-commit";
    break;
  case "unknown-root-field":
    manifest.created_at = "not-allowed";
    break;
  case "missing-root-field":
    delete manifest.revision;
    break;
  case "unknown-artifact":
    manifest.artifacts[0].name = "private-component";
    break;
  case "missing-artifact":
    manifest.artifacts.pop();
    break;
  case "bad-digest":
    manifest.artifacts[0].sha256 = "not-a-digest";
    break;
  case "bad-mode":
    manifest.artifacts[0].mode = "0777";
    break;
  case "absolute-path":
    manifest.artifacts[0].path = "/tmp/kbase-server";
    break;
  case "traversal-path":
    manifest.artifacts[0].path = "../kbase-server";
    break;
  case "backslash-path":
    manifest.artifacts[0].path = "bundle\\kbase-server";
    break;
  case "unknown-artifact-field":
    manifest.artifacts[0].host = "not-allowed";
    break;
  case "missing-artifact-field":
    delete manifest.artifacts[0].mode;
    break;
  default:
    throw new Error(`unknown prepared mutation: ${mutation}`);
}

fs.writeFileSync(targetPath, `${JSON.stringify(manifest, null, 2)}\n`);
NODE
}

if [[ ! -x "$ASSEMBLER" ]]; then
  fail "assembler is missing or not executable: ${ASSEMBLER}"
fi
if [[ ! -x "$PREPARER" ]]; then
  fail "release preparer is missing or not executable: ${PREPARER}"
fi
if [[ ! -x "$SIGNATURE_HELPER" ]]; then
  fail "release signature helper is missing or not executable: ${SIGNATURE_HELPER}"
fi

command -v git >/dev/null 2>&1 || fail "git is required"
command -v node >/dev/null 2>&1 || fail "node is required"
command -v openssl >/dev/null 2>&1 || fail "OpenSSL is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kbase-release-manifest.XXXXXX")"
TMP_ROOT="$(cd "$TMP_ROOT" && pwd -P)"
trap 'rm -rf "$TMP_ROOT"' EXIT

OPENSSL_BIN="$(command -v openssl)"
SOURCE_PRIVATE_KEY="${TMP_ROOT}/source-private.pem"
SOURCE_PUBLIC_KEY="${TMP_ROOT}/source-public.pem"
PREPARED_PRIVATE_KEY="${TMP_ROOT}/prepared-private.pem"
PREPARED_PUBLIC_KEY="${TMP_ROOT}/prepared-public.pem"
WRONG_PRIVATE_KEY="${TMP_ROOT}/wrong-private.pem"
WRONG_PUBLIC_KEY="${TMP_ROOT}/wrong-public.pem"
"$OPENSSL_BIN" genpkey \
  -algorithm RSA \
  -pkeyopt rsa_keygen_bits:3072 \
  -out "$SOURCE_PRIVATE_KEY" >/dev/null 2>&1
"$OPENSSL_BIN" pkey \
  -in "$SOURCE_PRIVATE_KEY" \
  -pubout \
  -out "$SOURCE_PUBLIC_KEY" >/dev/null 2>&1
"$OPENSSL_BIN" genpkey \
  -algorithm RSA \
  -pkeyopt rsa_keygen_bits:3072 \
  -out "$PREPARED_PRIVATE_KEY" >/dev/null 2>&1
"$OPENSSL_BIN" pkey \
  -in "$PREPARED_PRIVATE_KEY" \
  -pubout \
  -out "$PREPARED_PUBLIC_KEY" >/dev/null 2>&1
"$OPENSSL_BIN" genpkey \
  -algorithm RSA \
  -pkeyopt rsa_keygen_bits:3072 \
  -out "$WRONG_PRIVATE_KEY" >/dev/null 2>&1
"$OPENSSL_BIN" pkey \
  -in "$WRONG_PRIVATE_KEY" \
  -pubout \
  -out "$WRONG_PUBLIC_KEY" >/dev/null 2>&1
chmod 0600 \
  "$SOURCE_PRIVATE_KEY" \
  "$PREPARED_PRIVATE_KEY" \
  "$WRONG_PRIVATE_KEY"
chmod 0644 \
  "$SOURCE_PUBLIC_KEY" \
  "$PREPARED_PUBLIC_KEY" \
  "$WRONG_PUBLIC_KEY"

assemble_fixture() {
  repo="$1"
  revision="$2"
  output_dir="$3"
  set +e
  "$ASSEMBLER" create \
    --repo "$repo" \
    --revision "$revision" \
    --output-dir "$output_dir"
  create_status="$?"
  set -e
  [[ "$create_status" -eq 0 ]] || return "$create_status"
  [[ ! -e "${output_dir}/MANIFEST.sig" ]] ||
    fail "source assembly unexpectedly accessed a signing boundary"
  "$SIGNATURE_HELPER" sign \
    --manifest "${output_dir}/release-manifest.json" \
    --signature "${output_dir}/MANIFEST.sig" \
    --signing-key "$SOURCE_PRIVATE_KEY" \
    --openssl-bin "$OPENSSL_BIN"
}

verify_source_fixture() {
  manifest="$1"
  trusted_public_key="${2:-$SOURCE_PUBLIC_KEY}"
  "$ASSEMBLER" verify \
    --manifest "$manifest" \
    --trusted-public-key "$trusted_public_key" \
    --openssl-bin "$OPENSSL_BIN"
}

verify_prepared_fixture() {
  manifest="$1"
  trusted_public_key="${2:-$PREPARED_PUBLIC_KEY}"
  "$PREPARER" verify \
    --manifest "$manifest" \
    --trusted-public-key "$trusted_public_key" \
    --node-bin "${FAKE_BIN}/node" \
    --tar-bin "${FAKE_BIN}/tar" \
    --gzip-bin "${FAKE_BIN}/gzip" \
    --openssl-bin "$OPENSSL_BIN"
}

FIXTURE_REPO="${TMP_ROOT}/fixture"
OUTPUT_ONE="${TMP_ROOT}/release-one"
OUTPUT_TWO="${TMP_ROOT}/release-two"
CONFLICT_TARGET="${TMP_ROOT}/conflict-target"
mkdir -p "$FIXTURE_REPO"

git -C "$FIXTURE_REPO" init -q
git -C "$FIXTURE_REPO" config user.name "Release Smoke"
git -C "$FIXTURE_REPO" config user.email "release-smoke@example.invalid"
printf 'fixture source\n' >"${FIXTURE_REPO}/README.md"
mkdir -p "${FIXTURE_REPO}/nested"
printf 'package fixture\n' >"${FIXTURE_REPO}/nested/package.txt"
mkdir -p \
  "${FIXTURE_REPO}/frontend/scripts" \
  "${FIXTURE_REPO}/frontend-web/scripts" \
  "${FIXTURE_REPO}/deploy/nginx" \
  "${FIXTURE_REPO}/cmd/kbase-server"
printf '{"name":"fixture","scripts":{"build":"fixture-build"}}\n' \
  >"${FIXTURE_REPO}/frontend/package.json"
printf '{"name":"fixture","lockfileVersion":3,"packages":{}}\n' \
  >"${FIXTURE_REPO}/frontend/package-lock.json"
cat >"${FIXTURE_REPO}/frontend/scripts/fixture-smoke.mjs" <<'FIXTURE_SMOKE'
import fs from "node:fs";

if (process.env.RELEASE_GATE_LOG) {
  fs.appendFileSync(process.env.RELEASE_GATE_LOG, "frontend-smoke\n");
}
FIXTURE_SMOKE
printf '<!doctype html><title>fixture web</title>\n' \
  >"${FIXTURE_REPO}/frontend-web/index.html"
printf 'globalThis.fixtureWeb = true;\n' \
  >"${FIXTURE_REPO}/frontend-web/app.js"
cat >"${FIXTURE_REPO}/frontend-web/scripts/fixture-web-smoke.mjs" <<'FIXTURE_WEB_SMOKE'
import fs from "node:fs";

if (process.env.RELEASE_GATE_LOG) {
  fs.appendFileSync(process.env.RELEASE_GATE_LOG, "web-smoke\n");
}
FIXTURE_WEB_SMOKE
printf 'location /api/ { proxy_pass http://127.0.0.1:__KBASE_PORT__; }\n' \
  >"${FIXTURE_REPO}/deploy/nginx/kbase.locations.conf.template"
cat >"${FIXTURE_REPO}/deploy/nginx/render-kbase-config.sh" <<'FIXTURE_RENDERER'
#!/usr/bin/env bash
set -euo pipefail
cp "$1" "$2"
FIXTURE_RENDERER
chmod 755 "${FIXTURE_REPO}/deploy/nginx/render-kbase-config.sh"
printf 'package main\nfunc main() {}\n' \
  >"${FIXTURE_REPO}/cmd/kbase-server/main.go"
printf 'module example.invalid/release-fixture\n\ngo 1.22\n' \
  >"${FIXTURE_REPO}/go.mod"
git -C "$FIXTURE_REPO" add \
  README.md \
  nested/package.txt \
  frontend \
  frontend-web \
  deploy \
  cmd \
  go.mod
git -C "$FIXTURE_REPO" commit -q -m "fixture"

REVISION="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"

expect_failure "source create rejects signing key" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "${TMP_ROOT}/unsigned-source" \
  --signing-key "$SOURCE_PRIVATE_KEY"
assemble_fixture "$FIXTURE_REPO" HEAD "$OUTPUT_ONE"

MANIFEST_ONE="${OUTPUT_ONE}/release-manifest.json"
SIGNATURE_ONE="${OUTPUT_ONE}/MANIFEST.sig"
ARCHIVE_ONE="${OUTPUT_ONE}/source.tar.gz"
WEAK_PRIVATE_KEY="${TMP_ROOT}/weak-private.pem"
WEAK_PUBLIC_KEY="${TMP_ROOT}/weak-public.pem"
WEAK_SIGNATURE="${TMP_ROOT}/weak-signature"
"$OPENSSL_BIN" genpkey \
  -algorithm RSA \
  -pkeyopt rsa_keygen_bits:2048 \
  -out "$WEAK_PRIVATE_KEY" >/dev/null 2>&1
"$OPENSSL_BIN" pkey \
  -in "$WEAK_PRIVATE_KEY" \
  -pubout \
  -out "$WEAK_PUBLIC_KEY" >/dev/null 2>&1
chmod 0600 "$WEAK_PRIVATE_KEY"
chmod 0644 "$WEAK_PUBLIC_KEY"
expect_failure "weak signing key rejected" \
  "$SIGNATURE_HELPER" sign \
  --manifest "$MANIFEST_ONE" \
  --signature "$WEAK_SIGNATURE" \
  --signing-key "$WEAK_PRIVATE_KEY" \
  --openssl-bin "$OPENSSL_BIN"
"$OPENSSL_BIN" dgst \
  -sha256 \
  -sign "$WEAK_PRIVATE_KEY" \
  -out "$WEAK_SIGNATURE" \
  "$MANIFEST_ONE"
expect_failure "weak trusted public key rejected" \
  "$SIGNATURE_HELPER" verify \
  --manifest "$MANIFEST_ONE" \
  --signature "$WEAK_SIGNATURE" \
  --trusted-public-key "$WEAK_PUBLIC_KEY" \
  --openssl-bin "$OPENSSL_BIN"
[[ -f "$MANIFEST_ONE" ]] || fail "create did not write release-manifest.json"
[[ -f "$SIGNATURE_ONE" ]] || fail "create did not write MANIFEST.sig"
[[ -f "$ARCHIVE_ONE" ]] || fail "create did not write source.tar.gz"

verify_source_fixture "$MANIFEST_ONE"
expect_failure "source verify requires trusted public key" \
  "$ASSEMBLER" verify \
  --manifest "$MANIFEST_ONE" \
  --openssl-bin "$OPENSSL_BIN"
expect_failure "source release wrong key rejection" \
  verify_source_fixture "$MANIFEST_ONE" "$WRONG_PUBLIC_KEY"
cp "$SIGNATURE_ONE" "${TMP_ROOT}/source-signature.clean"
printf 'tampered signature\n' >>"$SIGNATURE_ONE"
expect_failure "source release signature tamper rejection" \
  verify_source_fixture "$MANIFEST_ONE"
cp "${TMP_ROOT}/source-signature.clean" "$SIGNATURE_ONE"
verify_source_fixture "$MANIFEST_ONE"

for key_path in \
  "$SOURCE_PRIVATE_KEY" \
  "$SOURCE_PUBLIC_KEY" \
  "$PREPARED_PRIVATE_KEY" \
  "$PREPARED_PUBLIC_KEY"
do
  key_basename="${key_path##*/}"
  [[ ! -e "${OUTPUT_ONE}/${key_basename}" ]] ||
    fail "release output leaked key material: ${key_basename}"
done
if grep -E -R -q -- '-----BEGIN .*PRIVATE KEY-----|-----BEGIN PUBLIC KEY-----' \
  "$OUTPUT_ONE"
then
  fail "source release output contains key material"
fi

node - "$MANIFEST_ONE" "$SCHEMA" "$REVISION" "$TMP_ROOT" <<'NODE'
const fs = require("fs");
const path = require("path");

const manifestPath = process.argv[2];
const expectedSchema = process.argv[3];
const expectedRevision = process.argv[4];
const privateRoot = process.argv[5];
const raw = fs.readFileSync(manifestPath, "utf8");
const manifest = JSON.parse(raw);
const expectedKeys = ["artifact", "revision", "schema", "sha256"];
const actualKeys = Object.keys(manifest).sort();

if (JSON.stringify(actualKeys) !== JSON.stringify(expectedKeys)) {
  throw new Error(`unexpected manifest keys: ${actualKeys.join(",")}`);
}
if (manifest.schema !== expectedSchema) {
  throw new Error(`unexpected schema: ${manifest.schema}`);
}
if (manifest.revision !== expectedRevision) {
  throw new Error(`revision is not canonical: ${manifest.revision}`);
}
if (manifest.artifact !== "source.tar.gz" || path.isAbsolute(manifest.artifact)) {
  throw new Error(`artifact path is not stable and relative: ${manifest.artifact}`);
}
if (!/^[0-9a-f]{64}$/.test(manifest.sha256)) {
  throw new Error(`digest is malformed: ${manifest.sha256}`);
}
if (raw.includes(privateRoot)) {
  throw new Error("manifest leaked the temporary fixture path");
}
NODE

tar -tzf "$ARCHIVE_ONE" >"${TMP_ROOT}/archive-entries.txt"
if [[ ! -s "${TMP_ROOT}/archive-entries.txt" ]]; then
  fail "source archive is empty"
fi
while IFS= read -r entry; do
  case "$entry" in
    "${ARCHIVE_PREFIX}"*) ;;
    *) fail "archive entry does not use stable prefix: ${entry}" ;;
  esac
done <"${TMP_ROOT}/archive-entries.txt"

assemble_fixture "$FIXTURE_REPO" "$REVISION" "$OUTPUT_TWO"
cmp "$MANIFEST_ONE" "${OUTPUT_TWO}/release-manifest.json" ||
  fail "manifest is not deterministic for the same revision"
cmp "$SIGNATURE_ONE" "${OUTPUT_TWO}/MANIFEST.sig" ||
  fail "signature is not deterministic for the same revision"
cmp "$ARCHIVE_ONE" "${OUTPUT_TWO}/source.tar.gz" ||
  fail "archive is not deterministic for the same revision"

FAKE_BIN="${TMP_ROOT}/fake-bin"
POISON_BIN="${TMP_ROOT}/poison-bin"
GATE_LOG="${TMP_ROOT}/release-gates.log"
PREPARED_ONE="${TMP_ROOT}/prepared-one"
PREPARED_TWO="${TMP_ROOT}/prepared-two"
PREPARED_CONFLICT="${TMP_ROOT}/prepared-conflict"
mkdir -p "$FAKE_BIN" "$POISON_BIN"
export RELEASE_GATE_LOG="$GATE_LOG"
export RELEASE_REAL_TAR="$(command -v tar)"
export RELEASE_REAL_GZIP="$(command -v gzip)"
export RELEASE_REAL_NODE="$(command -v node)"
export RELEASE_REAL_OPENSSL="$OPENSSL_BIN"

cat >"${FAKE_BIN}/uname" <<'FAKE_UNAME'
#!/usr/bin/env bash
set -euo pipefail
printf 'uname:%s\n' "$*" >>"$RELEASE_GATE_LOG"
[[ "${1:-}" == "-s" ]] || exit 2
printf 'Linux\n'
FAKE_UNAME

cat >"${FAKE_BIN}/npm" <<'FAKE_NPM'
#!/usr/bin/env bash
set -euo pipefail
printf 'npm:%s:%s\n' "$PWD" "$*" >>"$RELEASE_GATE_LOG"
if [[ "${RELEASE_FAIL_GATE:-}" == "npm-ci" && "${1:-}" == "ci" ]]; then
  exit 17
fi
if [[ "${1:-}" == "run" && "${2:-}" == "build" ]]; then
  mkdir -p dist
  printf 'fixture vue build\n' >dist/app.js
fi
FAKE_NPM

cat >"${FAKE_BIN}/go" <<'FAKE_GO'
#!/usr/bin/env bash
set -euo pipefail
printf 'go:%s:%s:cgo=%s\n' \
  "$PWD" \
  "$*" \
  "${CGO_ENABLED:-}" \
  >>"$RELEASE_GATE_LOG"
if [[ "${1:-}" == "build" ]]; then
  output=""
  while [[ "$#" -gt 0 ]]; do
    if [[ "$1" == "-o" ]]; then
      output="${2:-}"
      break
    fi
    shift
  done
  [[ -n "$output" ]] || exit 3
  printf '#!/usr/bin/env bash\nexit 0\n' >"$output"
  chmod 755 "$output"
fi
FAKE_GO

cat >"${FAKE_BIN}/nginx" <<'FAKE_NGINX'
#!/usr/bin/env bash
set -euo pipefail
printf 'nginx:%s\n' "$*" >>"$RELEASE_GATE_LOG"
FAKE_NGINX

cat >"${FAKE_BIN}/node" <<'FAKE_NODE'
#!/usr/bin/env bash
set -euo pipefail
printf 'node:%s\n' "$*" >>"$RELEASE_GATE_LOG"
exec "$RELEASE_REAL_NODE" "$@"
FAKE_NODE

cat >"${POISON_BIN}/node" <<'POISON_NODE'
#!/usr/bin/env bash
set -euo pipefail
printf 'poison-node\n' >>"$RELEASE_GATE_LOG"
printf 'poison PATH node invoked\n' >&2
exit 91
POISON_NODE

cat >"${FAKE_BIN}/tar" <<'FAKE_TAR'
#!/usr/bin/env bash
set -euo pipefail
printf 'tar:%s\n' "$*" >>"$RELEASE_GATE_LOG"
if [[ "${1:-}" == "--version" ]]; then
  printf 'tar (GNU tar) fixture\n'
  exit 0
fi
if "$RELEASE_REAL_TAR" --version 2>&1 | grep -q 'GNU tar'; then
  exec "$RELEASE_REAL_TAR" "$@"
fi
filtered=()
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --sort=*|--mtime=*|--owner=*|--group=*|--numeric-owner|--full-time|\
    --quoting-style=escape|--no-same-owner|--no-same-permissions|\
    --delay-directory-restore)
      shift
      ;;
    *)
      filtered+=("$1")
      shift
      ;;
  esac
done
exec "$RELEASE_REAL_TAR" "${filtered[@]}"
FAKE_TAR

cat >"${FAKE_BIN}/gzip" <<'FAKE_GZIP'
#!/usr/bin/env bash
set -euo pipefail
printf 'gzip:%s\n' "$*" >>"$RELEASE_GATE_LOG"
exec "$RELEASE_REAL_GZIP" "$@"
FAKE_GZIP

cat >"${FAKE_BIN}/openssl-staged" <<'FAKE_OPENSSL'
#!/usr/bin/env bash
set -euo pipefail
printf 'openssl:%s\n' "$*" >>"$RELEASE_GATE_LOG"
for argument in "$@"; do
  case "$argument" in
    */source-input/release-manifest.json)
      "$RELEASE_REAL_NODE" - "$argument" "$RELEASE_GATE_LOG" <<'NODE'
const fs = require("fs");
const path = require("path");

const mode = (
  fs.statSync(path.dirname(process.argv[2])).mode & 0o777
).toString(8);
fs.appendFileSync(process.argv[3], `source-stage-mode:${mode}\n`);
NODE
      ;;
  esac
done
if [[ -n "${RELEASE_MUTATE_SOURCE_ARCHIVE:-}" ]] &&
  [[ ! -e "${RELEASE_MUTATION_MARKER:-}" ]]
then
  printf 'mutated after staging\n' >>"$RELEASE_MUTATE_SOURCE_ARCHIVE"
  printf 'mutated\n' >"$RELEASE_MUTATION_MARKER"
fi
exec "$RELEASE_REAL_OPENSSL" "$@"
FAKE_OPENSSL

cat >"${FAKE_BIN}/proxy-smoke.sh" <<'FAKE_PROXY'
#!/usr/bin/env bash
set -euo pipefail
printf 'proxy:%s\n' "$PWD" >>"$RELEASE_GATE_LOG"
[[ -x "${KBASE_SERVER_BIN:-}" ]] || exit 4
[[ -x "${NGINX_BIN:-}" ]] || exit 5
"$NGINX_BIN" -v
FAKE_PROXY

chmod 755 \
  "${FAKE_BIN}/uname" \
  "${FAKE_BIN}/npm" \
  "${FAKE_BIN}/go" \
  "${FAKE_BIN}/nginx" \
  "${FAKE_BIN}/node" \
  "${FAKE_BIN}/tar" \
  "${FAKE_BIN}/gzip" \
  "${FAKE_BIN}/openssl-staged" \
  "${FAKE_BIN}/proxy-smoke.sh" \
  "${POISON_BIN}/node"

prepare_fixture() {
  source_manifest="$1"
  output_dir="$2"
  chmod 000 "$PREPARED_PRIVATE_KEY"
  set +e
  PATH="${POISON_BIN}:$PATH" "$PREPARER" create \
    --source-manifest "$source_manifest" \
    --output-dir "$output_dir" \
    --source-public-key "$SOURCE_PUBLIC_KEY" \
    --openssl-bin "${PREPARE_OPENSSL_BIN:-$OPENSSL_BIN}" \
    --go-bin "${FAKE_BIN}/go" \
    --npm-bin "${FAKE_BIN}/npm" \
    --node-bin "${FAKE_BIN}/node" \
    --tar-bin "${FAKE_BIN}/tar" \
    --gzip-bin "${FAKE_BIN}/gzip" \
    --nginx-bin "${FAKE_BIN}/nginx" \
    --uname-bin "${FAKE_BIN}/uname" \
    --proxy-smoke-script "${FAKE_BIN}/proxy-smoke.sh"
  create_status="$?"
  set -e
  chmod 0600 "$PREPARED_PRIVATE_KEY"
  [[ "$create_status" -eq 0 ]] || return "$create_status"
  [[ ! -e "${output_dir}/MANIFEST.sig" ]] ||
    fail "prepare create unexpectedly signed the release"
  "$SIGNATURE_HELPER" sign \
    --manifest "${output_dir}/prepared-manifest.json" \
    --signature "${output_dir}/MANIFEST.sig" \
    --signing-key "$PREPARED_PRIVATE_KEY" \
    --openssl-bin "$OPENSSL_BIN"
}

expect_source_quota_failure() {
  description="$1"
  quota_name="$2"
  quota_value="$3"
  output_dir="$4"
  export "${quota_name}=${quota_value}"
  : >"$GATE_LOG"
  expect_failure "$description" \
    prepare_fixture "$MANIFEST_ONE" "$output_dir"
  unset "$quota_name"
  if grep -E -q \
    '^(npm:|go:|frontend-smoke$|web-smoke$|proxy:|nginx:)' \
    "$GATE_LOG"
  then
    fail "${description}: build gates ran after quota rejection"
  fi
  if [[ "$quota_name" == "KBASE_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES" ]] &&
    grep -E -q '^(tar:|gzip:)' "$GATE_LOG"
  then
    fail "${description}: archive tools ran before compressed-size rejection"
  fi
  [[ ! -e "$output_dir" ]] ||
    fail "${description}: quota rejection published a release"
}

grep -q 'release preparation must not run as root' "$PREPARER" ||
  fail "prepare does not enforce the non-root contract"
expect_failure "prepare create requires source public key" \
  "$PREPARER" create \
  --source-manifest "$MANIFEST_ONE" \
  --output-dir "${TMP_ROOT}/missing-source-key-prepared"
expect_failure "prepare create rejects signing key" \
  "$PREPARER" create \
  --source-manifest "$MANIFEST_ONE" \
  --source-public-key "$SOURCE_PUBLIC_KEY" \
  --signing-key "$PREPARED_PRIVATE_KEY" \
  --output-dir "${TMP_ROOT}/missing-signing-key-prepared"
expect_failure "prepare rejects private-key signing command" \
  "$PREPARER" sign \
  --manifest "${TMP_ROOT}/missing-signing-key-prepared/prepared-manifest.json" \
  --signing-key "$PREPARED_PRIVATE_KEY"

UNSAFE_REPO="${TMP_ROOT}/unsafe-fixture"
UNSAFE_EXTERNAL="${TMP_ROOT}/unsafe-external-frontend"
UNSAFE_SOURCE="${TMP_ROOT}/unsafe-source-release"
mkdir -p \
  "$UNSAFE_REPO" \
  "${UNSAFE_EXTERNAL}/scripts" \
  "${UNSAFE_REPO}/deploy" \
  "${UNSAFE_REPO}/cmd"
printf '{"name":"unsafe-fixture"}\n' \
  >"${UNSAFE_EXTERNAL}/package.json"
printf '{"name":"unsafe-fixture","lockfileVersion":3,"packages":{}}\n' \
  >"${UNSAFE_EXTERNAL}/package-lock.json"
printf 'export {};\n' \
  >"${UNSAFE_EXTERNAL}/scripts/unsafe-smoke.mjs"
printf 'outside must remain unchanged\n' \
  >"${UNSAFE_EXTERNAL}/marker.txt"
ln -s "$UNSAFE_EXTERNAL" "${UNSAFE_REPO}/frontend"
cp -R "${FIXTURE_REPO}/frontend-web" "${UNSAFE_REPO}/frontend-web"
cp -R "${FIXTURE_REPO}/deploy/nginx" "${UNSAFE_REPO}/deploy/nginx"
cp -R "${FIXTURE_REPO}/cmd/kbase-server" "${UNSAFE_REPO}/cmd/kbase-server"
cp "${FIXTURE_REPO}/go.mod" "${UNSAFE_REPO}/go.mod"
git -C "$UNSAFE_REPO" init -q
git -C "$UNSAFE_REPO" config user.name "Unsafe Release Smoke"
git -C "$UNSAFE_REPO" config user.email "unsafe-release@example.invalid"
git -C "$UNSAFE_REPO" add frontend frontend-web deploy cmd go.mod
git -C "$UNSAFE_REPO" commit -q -m "unsafe symlink fixture"
assemble_fixture "$UNSAFE_REPO" HEAD "$UNSAFE_SOURCE"

: >"$GATE_LOG"
expect_failure "unsafe source symlink rejection" \
  prepare_fixture \
  "${UNSAFE_SOURCE}/release-manifest.json" \
  "${TMP_ROOT}/unsafe-prepared"
if grep -E -q \
  '^(npm:|go:|frontend-smoke$|web-smoke$|proxy:|nginx:)' \
  "$GATE_LOG"
then
  fail "build gates ran for an unsafe source archive"
fi
grep -q '^outside must remain unchanged$' \
  "${UNSAFE_EXTERNAL}/marker.txt" ||
  fail "unsafe source preparation changed the external marker"
[[ ! -e "${UNSAFE_EXTERNAL}/dist" ]] ||
  fail "unsafe source preparation wrote outside its staging directory"
[[ ! -e "${TMP_ROOT}/unsafe-prepared" ]] ||
  fail "unsafe source archive produced a prepared release"

cp -R "$OUTPUT_ONE" "${TMP_ROOT}/tampered-source"
printf 'tampered before prepare\n' \
  >>"${TMP_ROOT}/tampered-source/source.tar.gz"
: >"$GATE_LOG"
expect_failure "tampered source rejected before build" \
  prepare_fixture \
  "${TMP_ROOT}/tampered-source/release-manifest.json" \
  "${TMP_ROOT}/tampered-prepared"
if grep -E -q \
  '^(npm:|go:|frontend-smoke$|web-smoke$|proxy:|nginx:|tar:|gzip:|uname:)' \
  "$GATE_LOG"
then
  fail "build commands ran before tampered source rejection"
fi
[[ ! -e "${TMP_ROOT}/tampered-prepared" ]] ||
  fail "tampered source produced a prepared release"

expect_source_quota_failure \
  "source compressed-size quota" \
  KBASE_SOURCE_ARCHIVE_MAX_COMPRESSED_BYTES \
  1 \
  "${TMP_ROOT}/compressed-quota-prepared"
expect_source_quota_failure \
  "source member-count quota" \
  KBASE_SOURCE_ARCHIVE_MAX_MEMBERS \
  1 \
  "${TMP_ROOT}/member-quota-prepared"
expect_source_quota_failure \
  "source per-file quota" \
  KBASE_SOURCE_ARCHIVE_MAX_FILE_BYTES \
  1 \
  "${TMP_ROOT}/file-quota-prepared"
expect_source_quota_failure \
  "source expanded-size quota" \
  KBASE_SOURCE_ARCHIVE_MAX_EXPANDED_BYTES \
  1 \
  "${TMP_ROOT}/expanded-quota-prepared"
expect_source_quota_failure \
  "source quota cannot exceed the compiled ceiling" \
  KBASE_SOURCE_ARCHIVE_MAX_MEMBERS \
  100001 \
  "${TMP_ROOT}/raised-quota-prepared"

STAGED_SOURCE="${TMP_ROOT}/staged-source-release"
STAGED_PREPARED="${TMP_ROOT}/staged-source-prepared"
cp -R "$OUTPUT_ONE" "$STAGED_SOURCE"
export RELEASE_MUTATE_SOURCE_ARCHIVE="${STAGED_SOURCE}/source.tar.gz"
export RELEASE_MUTATION_MARKER="${TMP_ROOT}/source-mutated.marker"
export PREPARE_OPENSSL_BIN="${FAKE_BIN}/openssl-staged"
: >"$GATE_LOG"
prepare_fixture \
  "${STAGED_SOURCE}/release-manifest.json" \
  "$STAGED_PREPARED"
unset PREPARE_OPENSSL_BIN
unset RELEASE_MUTATION_MARKER
unset RELEASE_MUTATE_SOURCE_ARCHIVE
[[ -f "${TMP_ROOT}/source-mutated.marker" ]] ||
  fail "staged-consumption test did not mutate the original source"
grep -q '^source-stage-mode:700$' "$GATE_LOG" ||
  fail "source inputs were not verified from a private 0700 staging directory"
expect_failure "mutated original source rejection" \
  verify_source_fixture "${STAGED_SOURCE}/release-manifest.json"
verify_prepared_fixture "${STAGED_PREPARED}/prepared-manifest.json"

: >"$GATE_LOG"
prepare_fixture "$MANIFEST_ONE" "$PREPARED_ONE"
PREPARED_MANIFEST="${PREPARED_ONE}/prepared-manifest.json"
[[ -f "$PREPARED_MANIFEST" ]] ||
  fail "prepare did not write prepared-manifest.json"
PREPARED_SIGNATURE="${PREPARED_ONE}/MANIFEST.sig"
[[ -f "$PREPARED_SIGNATURE" ]] ||
  fail "prepare did not write MANIFEST.sig"

SIGN_BOUNDARY_RELEASE="${TMP_ROOT}/prepared-sign-boundary"
cp -R "$PREPARED_ONE" "$SIGN_BOUNDARY_RELEASE"
rm "${SIGN_BOUNDARY_RELEASE}/MANIFEST.sig"
if "$PREPARER" sign \
  --manifest "${SIGN_BOUNDARY_RELEASE}/prepared-manifest.json" \
  --signing-key "$PREPARED_PRIVATE_KEY" \
  --node-bin "${FAKE_BIN}/node" \
  --tar-bin "${FAKE_BIN}/tar" \
  --gzip-bin "${FAKE_BIN}/gzip" \
  --openssl-bin "$OPENSSL_BIN" >/dev/null 2>&1; then
  fail "prepare command unexpectedly accepted a private signing key"
fi
verify_prepared_fixture "$PREPARED_MANIFEST"
expect_failure "prepared verify requires trusted public key" \
  "$PREPARER" verify \
  --manifest "$PREPARED_MANIFEST" \
  --openssl-bin "$OPENSSL_BIN"
expect_failure "prepared release wrong key rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST" "$WRONG_PUBLIC_KEY"
cp "$PREPARED_SIGNATURE" "${TMP_ROOT}/prepared-signature.clean"
printf 'tampered signature\n' >>"$PREPARED_SIGNATURE"
expect_failure "prepared release signature tamper rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST"
cp "${TMP_ROOT}/prepared-signature.clean" "$PREPARED_SIGNATURE"
verify_prepared_fixture "$PREPARED_MANIFEST"
for key_path in \
  "$SOURCE_PRIVATE_KEY" \
  "$SOURCE_PUBLIC_KEY" \
  "$PREPARED_PRIVATE_KEY" \
  "$PREPARED_PUBLIC_KEY"
do
  key_basename="${key_path##*/}"
  [[ ! -e "${PREPARED_ONE}/${key_basename}" ]] ||
    fail "prepared output leaked key material: ${key_basename}"
done
if grep -E -R -q -- '-----BEGIN .*PRIVATE KEY-----|-----BEGIN PUBLIC KEY-----' \
  "$PREPARED_ONE"
then
  fail "prepared release output contains key material"
fi

for gate in \
  '^uname:-s$' \
  '^npm:.*:ci$' \
  '^npm:.*:run build$' \
  '^frontend-smoke$' \
  '^node:- .*release-manifest\.json dedao-kbase-source-release/v1$' \
  '^node:--check .*/frontend-web/app\.js$' \
  '^web-smoke$' \
  '^go:.*:test \./\.\.\.:cgo=$' \
  '^go:.*:build -trimpath -ldflags -X main\.buildRevision=[0-9a-f]{40} -o .* \./cmd/kbase-server:cgo=1$' \
  '^proxy:.*$' \
  '^nginx:-v$' \
  '^tar:--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C .* -cf - frontend-web$' \
  '^gzip:-n$'
do
  grep -E -q "$gate" "$GATE_LOG" ||
    fail "release gate was not invoked: $gate"
done

node - \
  "$PREPARED_MANIFEST" \
  "$PREPARED_SCHEMA" \
  "$REVISION" \
  "$TMP_ROOT" <<'NODE'
const fs = require("fs");
const path = require("path");

const manifestPath = process.argv[2];
const expectedSchema = process.argv[3];
const expectedRevision = process.argv[4];
const privateRoot = process.argv[5];
const raw = fs.readFileSync(manifestPath, "utf8");
const manifest = JSON.parse(raw);

if (
  JSON.stringify(Object.keys(manifest).sort()) !==
  JSON.stringify(["artifacts", "revision", "schema"])
) {
  throw new Error("prepared manifest root fields are not minimal");
}
if (manifest.schema !== expectedSchema || manifest.revision !== expectedRevision) {
  throw new Error("prepared manifest identity is incorrect");
}
const expected = new Map([
  ["kbase-server", ["bundle/kbase-server", "0755"]],
  ["web", ["bundle/web.tar.gz", "0644"]],
  ["nginx-template", ["bundle/kbase.locations.conf.template", "0644"]],
  ["config-renderer", ["bundle/render-kbase-config.sh", "0755"]],
]);
if (!Array.isArray(manifest.artifacts) || manifest.artifacts.length !== 4) {
  throw new Error("prepared manifest must contain four artifacts");
}
for (const artifact of manifest.artifacts) {
  if (
    JSON.stringify(Object.keys(artifact).sort()) !==
    JSON.stringify(["mode", "name", "path", "sha256"])
  ) {
    throw new Error(`artifact fields are not minimal: ${artifact.name}`);
  }
  const specification = expected.get(artifact.name);
  if (
    !specification ||
    artifact.path !== specification[0] ||
    artifact.mode !== specification[1] ||
    path.isAbsolute(artifact.path) ||
    !/^[0-9a-f]{64}$/.test(artifact.sha256)
  ) {
    throw new Error(`invalid prepared artifact: ${artifact.name}`);
  }
}
if (raw.includes(privateRoot)) {
  throw new Error("prepared manifest leaked the fixture path");
}
NODE

: >"$GATE_LOG"
"$PREPARER" verify \
  --node-bin "${FAKE_BIN}/node" \
  --tar-bin "${FAKE_BIN}/tar" \
  --gzip-bin "${FAKE_BIN}/gzip" \
  --manifest "$PREPARED_MANIFEST" \
  --trusted-public-key "$PREPARED_PUBLIC_KEY" \
  --openssl-bin "$OPENSSL_BIN"
grep -E -q '^node:- .*prepared-manifest\.json ' "$GATE_LOG" ||
  fail "explicit verifier Node wrapper was not invoked"

: >"$GATE_LOG"
prepare_fixture "$MANIFEST_ONE" "$PREPARED_TWO"
cmp \
  "${PREPARED_ONE}/bundle/web.tar.gz" \
  "${PREPARED_TWO}/bundle/web.tar.gz" ||
  fail "prepared web archive is not deterministic"
cmp \
  "${PREPARED_ONE}/prepared-manifest.json" \
  "${PREPARED_TWO}/prepared-manifest.json" ||
  fail "prepared manifest is not deterministic"
cmp \
  "${PREPARED_ONE}/MANIFEST.sig" \
  "${PREPARED_TWO}/MANIFEST.sig" ||
  fail "prepared signature is not deterministic"

mkdir "$PREPARED_CONFLICT"
printf 'do not replace\n' >"${PREPARED_CONFLICT}/marker.txt"
: >"$GATE_LOG"
expect_failure "existing prepared output rejection" \
  prepare_fixture "$MANIFEST_ONE" "$PREPARED_CONFLICT"
grep -q '^do not replace$' "${PREPARED_CONFLICT}/marker.txt" ||
  fail "existing prepared output was modified"
if grep -E -q \
  '^(npm:|go:|frontend-smoke$|web-smoke$|proxy:|nginx:|tar:|gzip:|uname:)' \
  "$GATE_LOG"
then
  fail "build gates ran for an existing output target"
fi
if grep -q '^poison-node$' "$GATE_LOG"; then
  fail "source verification ignored the explicit Node tool"
fi

export RELEASE_FAIL_GATE="npm-ci"
expect_failure "build failure propagation" \
  prepare_fixture "$MANIFEST_ONE" "${TMP_ROOT}/failed-prepared"
unset RELEASE_FAIL_GATE
[[ ! -e "${TMP_ROOT}/failed-prepared" ]] ||
  fail "failed build published a prepared release"

cp "${PREPARED_ONE}/bundle/kbase-server" "${TMP_ROOT}/kbase-server.clean"
printf 'tampered prepared binary\n' \
  >>"${PREPARED_ONE}/bundle/kbase-server"
expect_failure "tampered prepared artifact rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST"
cp "${TMP_ROOT}/kbase-server.clean" \
  "${PREPARED_ONE}/bundle/kbase-server"
chmod 755 "${PREPARED_ONE}/bundle/kbase-server"
verify_prepared_fixture "$PREPARED_MANIFEST"

OVERSIZED_PREPARED="${TMP_ROOT}/oversized-prepared"
OVERSIZED_ERROR="${TMP_ROOT}/oversized-prepared.error"
cp -R "$PREPARED_ONE" "$OVERSIZED_PREPARED"
node - "${OVERSIZED_PREPARED}/bundle/kbase.locations.conf.template" <<'NODE'
const fs = require("fs");
fs.writeFileSync(process.argv[2], Buffer.alloc(1024 * 1024 + 1, 0x61));
NODE
if verify_prepared_fixture \
  "${OVERSIZED_PREPARED}/prepared-manifest.json" \
  >"$OVERSIZED_ERROR" 2>&1
then
  fail "oversized prepared artifact unexpectedly verified"
fi
grep -q 'prepared artifact exceeds byte limit: nginx-template' \
  "$OVERSIZED_ERROR" ||
  fail "oversized prepared artifact was not rejected by its byte limit"

printf 'unexpected\n' >"${PREPARED_ONE}/bundle/extra.txt"
expect_failure "extra prepared bundle file rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST"
rm "${PREPARED_ONE}/bundle/extra.txt"

chmod 644 "${PREPARED_ONE}/bundle/kbase-server"
expect_failure "prepared executable mode rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST"
chmod 755 "${PREPARED_ONE}/bundle/kbase-server"

mv \
  "${PREPARED_ONE}/bundle/render-kbase-config.sh" \
  "${TMP_ROOT}/render-kbase-config.clean"
ln -s \
  "${TMP_ROOT}/render-kbase-config.clean" \
  "${PREPARED_ONE}/bundle/render-kbase-config.sh"
expect_failure "prepared symlink rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST"
rm "${PREPARED_ONE}/bundle/render-kbase-config.sh"
mv \
  "${TMP_ROOT}/render-kbase-config.clean" \
  "${PREPARED_ONE}/bundle/render-kbase-config.sh"

mv \
  "${PREPARED_ONE}/bundle/web.tar.gz" \
  "${TMP_ROOT}/web.tar.gz.clean"
expect_failure "missing prepared artifact rejection" \
  verify_prepared_fixture "$PREPARED_MANIFEST"
mv \
  "${TMP_ROOT}/web.tar.gz.clean" \
  "${PREPARED_ONE}/bundle/web.tar.gz"

for mutation in \
  wrong-schema \
  bad-revision \
  unknown-root-field \
  missing-root-field \
  unknown-artifact \
  missing-artifact \
  bad-digest \
  bad-mode \
  absolute-path \
  traversal-path \
  backslash-path \
  unknown-artifact-field \
  missing-artifact-field
do
  variant="${PREPARED_ONE}/prepared-${mutation}.json"
  write_prepared_manifest_variant "$PREPARED_MANIFEST" "$variant" "$mutation"
  expect_failure "prepared ${mutation} rejection" \
    verify_prepared_fixture "$variant"
done

if find "$TMP_ROOT" -maxdepth 1 -name '.*.staging.*' -print | grep -q .; then
  fail "prepared release staging directory was not cleaned"
fi

printf 'existing release marker\n' >"${OUTPUT_ONE}/existing-release.txt"
cp "$MANIFEST_ONE" "${TMP_ROOT}/existing-manifest.json"
cp "$ARCHIVE_ONE" "${TMP_ROOT}/existing-source.tar.gz"
expect_failure "existing release directory rejection" \
  assemble_fixture "$FIXTURE_REPO" HEAD "$OUTPUT_ONE"
cmp "${TMP_ROOT}/existing-manifest.json" "$MANIFEST_ONE" ||
  fail "existing release manifest changed after rejected create"
cmp "${TMP_ROOT}/existing-source.tar.gz" "$ARCHIVE_ONE" ||
  fail "existing release archive changed after rejected create"
grep -q '^existing release marker$' "${OUTPUT_ONE}/existing-release.txt" ||
  fail "existing release marker changed after rejected create"

printf 'existing file\n' >"$CONFLICT_TARGET"
cp "$CONFLICT_TARGET" "${TMP_ROOT}/conflict-target.clean"
expect_failure "existing file target rejection" \
  assemble_fixture "$FIXTURE_REPO" HEAD "$CONFLICT_TARGET"
cmp "${TMP_ROOT}/conflict-target.clean" "$CONFLICT_TARGET" ||
  fail "existing file target changed after rejected create"

mkdir -p "${TMP_ROOT}/symlink-target"
printf 'symlink target marker\n' >"${TMP_ROOT}/symlink-target/marker.txt"
ln -s "${TMP_ROOT}/symlink-target" "${TMP_ROOT}/release-symlink"
expect_failure "existing symlink target rejection" \
  assemble_fixture \
  "$FIXTURE_REPO" \
  HEAD \
  "${TMP_ROOT}/release-symlink"
grep -q '^symlink target marker$' "${TMP_ROOT}/symlink-target/marker.txt" ||
  fail "symlink target changed after rejected create"
[[ ! -e "${TMP_ROOT}/symlink-target/source.tar.gz" ]] ||
  fail "rejected symlink target received release artifacts"

expect_failure "missing output parent rejection" \
  assemble_fixture \
  "$FIXTURE_REPO" \
  HEAD \
  "${TMP_ROOT}/missing-parent/release"
[[ ! -e "${TMP_ROOT}/missing-parent" ]] ||
  fail "create made a missing output parent"

expect_failure "dot basename rejection" \
  assemble_fixture "$FIXTURE_REPO" HEAD "${TMP_ROOT}/."
expect_failure "dot-dot basename rejection" \
  assemble_fixture "$FIXTURE_REPO" HEAD "${TMP_ROOT}/nested-parent/.."

if find "$TMP_ROOT" -maxdepth 1 -name '.*.staging.*' -print | grep -q .; then
  fail "release staging directory was not cleaned"
fi

printf 'dirty source\n' >>"${FIXTURE_REPO}/README.md"
expect_failure "dirty worktree rejection" \
  assemble_fixture "$FIXTURE_REPO" HEAD "${TMP_ROOT}/dirty-release"
git -C "$FIXTURE_REPO" checkout -q -- README.md

cp "$ARCHIVE_ONE" "${TMP_ROOT}/source.tar.gz.clean"
printf 'tampered\n' >>"$ARCHIVE_ONE"
expect_failure "tampered archive rejection" \
  verify_source_fixture "$MANIFEST_ONE"
cp "${TMP_ROOT}/source.tar.gz.clean" "$ARCHIVE_ONE"
verify_source_fixture "$MANIFEST_ONE"

for mutation in \
  wrong-schema \
  bad-revision \
  bad-digest \
  absolute-artifact \
  traversal-artifact \
  missing-artifact \
  unknown-field
do
  variant="${OUTPUT_ONE}/manifest-${mutation}.json"
  write_manifest_variant "$MANIFEST_ONE" "$variant" "$mutation"
  expect_failure "${mutation} rejection" \
    verify_source_fixture "$variant"
done

printf 'release manifest smoke: PASS\n'
