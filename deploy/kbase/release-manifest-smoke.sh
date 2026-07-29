#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSEMBLER="${SCRIPT_DIR}/assemble-release.sh"
PREPARER="${SCRIPT_DIR}/prepare-release.sh"
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

command -v git >/dev/null 2>&1 || fail "git is required"
command -v node >/dev/null 2>&1 || fail "node is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kbase-release-manifest.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

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

"$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "$OUTPUT_ONE"

MANIFEST_ONE="${OUTPUT_ONE}/release-manifest.json"
ARCHIVE_ONE="${OUTPUT_ONE}/source.tar.gz"
[[ -f "$MANIFEST_ONE" ]] || fail "create did not write release-manifest.json"
[[ -f "$ARCHIVE_ONE" ]] || fail "create did not write source.tar.gz"

"$ASSEMBLER" verify --manifest "$MANIFEST_ONE"

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

"$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision "$REVISION" \
  --output-dir "$OUTPUT_TWO"
cmp "$MANIFEST_ONE" "${OUTPUT_TWO}/release-manifest.json" ||
  fail "manifest is not deterministic for the same revision"
cmp "$ARCHIVE_ONE" "${OUTPUT_TWO}/source.tar.gz" ||
  fail "archive is not deterministic for the same revision"

FAKE_BIN="${TMP_ROOT}/fake-bin"
GATE_LOG="${TMP_ROOT}/release-gates.log"
PREPARED_ONE="${TMP_ROOT}/prepared-one"
PREPARED_TWO="${TMP_ROOT}/prepared-two"
PREPARED_CONFLICT="${TMP_ROOT}/prepared-conflict"
mkdir -p "$FAKE_BIN"
export RELEASE_GATE_LOG="$GATE_LOG"
export RELEASE_REAL_TAR="$(command -v tar)"
export RELEASE_REAL_GZIP="$(command -v gzip)"
export RELEASE_REAL_NODE="$(command -v node)"

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

cat >"${FAKE_BIN}/tar" <<'FAKE_TAR'
#!/usr/bin/env bash
set -euo pipefail
printf 'tar:%s\n' "$*" >>"$RELEASE_GATE_LOG"
filtered=()
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --sort=*|--mtime=*|--owner=*|--group=*|--numeric-owner)
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
  "${FAKE_BIN}/proxy-smoke.sh"

prepare_fixture() {
  source_manifest="$1"
  output_dir="$2"
  "$PREPARER" create \
    --source-manifest "$source_manifest" \
    --output-dir "$output_dir" \
    --go-bin "${FAKE_BIN}/go" \
    --npm-bin "${FAKE_BIN}/npm" \
    --node-bin "${FAKE_BIN}/node" \
    --tar-bin "${FAKE_BIN}/tar" \
    --gzip-bin "${FAKE_BIN}/gzip" \
    --nginx-bin "${FAKE_BIN}/nginx" \
    --uname-bin "${FAKE_BIN}/uname" \
    --proxy-smoke-script "${FAKE_BIN}/proxy-smoke.sh"
}

cp -R "$OUTPUT_ONE" "${TMP_ROOT}/tampered-source"
printf 'tampered before prepare\n' \
  >>"${TMP_ROOT}/tampered-source/source.tar.gz"
: >"$GATE_LOG"
expect_failure "tampered source rejected before build" \
  prepare_fixture \
  "${TMP_ROOT}/tampered-source/release-manifest.json" \
  "${TMP_ROOT}/tampered-prepared"
[[ ! -s "$GATE_LOG" ]] ||
  fail "build commands ran before tampered source rejection"
[[ ! -e "${TMP_ROOT}/tampered-prepared" ]] ||
  fail "tampered source produced a prepared release"

: >"$GATE_LOG"
prepare_fixture "$MANIFEST_ONE" "$PREPARED_ONE"
PREPARED_MANIFEST="${PREPARED_ONE}/prepared-manifest.json"
[[ -f "$PREPARED_MANIFEST" ]] ||
  fail "prepare did not write prepared-manifest.json"
"$PREPARER" verify --manifest "$PREPARED_MANIFEST"

for gate in \
  '^uname:-s$' \
  '^npm:.*:ci$' \
  '^npm:.*:run build$' \
  '^frontend-smoke$' \
  '^node:--check .*/frontend-web/app\.js$' \
  '^web-smoke$' \
  '^go:.*:test \./\.\.\.:cgo=$' \
  '^go:.*:build -trimpath -o .* \./cmd/kbase-server:cgo=1$' \
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
  --manifest "$PREPARED_MANIFEST"
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

mkdir "$PREPARED_CONFLICT"
printf 'do not replace\n' >"${PREPARED_CONFLICT}/marker.txt"
: >"$GATE_LOG"
expect_failure "existing prepared output rejection" \
  prepare_fixture "$MANIFEST_ONE" "$PREPARED_CONFLICT"
grep -q '^do not replace$' "${PREPARED_CONFLICT}/marker.txt" ||
  fail "existing prepared output was modified"
[[ ! -s "$GATE_LOG" ]] ||
  fail "build gates ran for an existing output target"

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
  "$PREPARER" verify --manifest "$PREPARED_MANIFEST"
cp "${TMP_ROOT}/kbase-server.clean" \
  "${PREPARED_ONE}/bundle/kbase-server"
chmod 755 "${PREPARED_ONE}/bundle/kbase-server"
"$PREPARER" verify --manifest "$PREPARED_MANIFEST"

printf 'unexpected\n' >"${PREPARED_ONE}/bundle/extra.txt"
expect_failure "extra prepared bundle file rejection" \
  "$PREPARER" verify --manifest "$PREPARED_MANIFEST"
rm "${PREPARED_ONE}/bundle/extra.txt"

chmod 644 "${PREPARED_ONE}/bundle/kbase-server"
expect_failure "prepared executable mode rejection" \
  "$PREPARER" verify --manifest "$PREPARED_MANIFEST"
chmod 755 "${PREPARED_ONE}/bundle/kbase-server"

mv \
  "${PREPARED_ONE}/bundle/render-kbase-config.sh" \
  "${TMP_ROOT}/render-kbase-config.clean"
ln -s \
  "${TMP_ROOT}/render-kbase-config.clean" \
  "${PREPARED_ONE}/bundle/render-kbase-config.sh"
expect_failure "prepared symlink rejection" \
  "$PREPARER" verify --manifest "$PREPARED_MANIFEST"
rm "${PREPARED_ONE}/bundle/render-kbase-config.sh"
mv \
  "${TMP_ROOT}/render-kbase-config.clean" \
  "${PREPARED_ONE}/bundle/render-kbase-config.sh"

mv \
  "${PREPARED_ONE}/bundle/web.tar.gz" \
  "${TMP_ROOT}/web.tar.gz.clean"
expect_failure "missing prepared artifact rejection" \
  "$PREPARER" verify --manifest "$PREPARED_MANIFEST"
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
    "$PREPARER" verify --manifest "$variant"
done

if find "$TMP_ROOT" -maxdepth 1 -name '.*.staging.*' -print | grep -q .; then
  fail "prepared release staging directory was not cleaned"
fi

printf 'existing release marker\n' >"${OUTPUT_ONE}/existing-release.txt"
cp "$MANIFEST_ONE" "${TMP_ROOT}/existing-manifest.json"
cp "$ARCHIVE_ONE" "${TMP_ROOT}/existing-source.tar.gz"
expect_failure "existing release directory rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "$OUTPUT_ONE"
cmp "${TMP_ROOT}/existing-manifest.json" "$MANIFEST_ONE" ||
  fail "existing release manifest changed after rejected create"
cmp "${TMP_ROOT}/existing-source.tar.gz" "$ARCHIVE_ONE" ||
  fail "existing release archive changed after rejected create"
grep -q '^existing release marker$' "${OUTPUT_ONE}/existing-release.txt" ||
  fail "existing release marker changed after rejected create"

printf 'existing file\n' >"$CONFLICT_TARGET"
cp "$CONFLICT_TARGET" "${TMP_ROOT}/conflict-target.clean"
expect_failure "existing file target rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "$CONFLICT_TARGET"
cmp "${TMP_ROOT}/conflict-target.clean" "$CONFLICT_TARGET" ||
  fail "existing file target changed after rejected create"

mkdir -p "${TMP_ROOT}/symlink-target"
printf 'symlink target marker\n' >"${TMP_ROOT}/symlink-target/marker.txt"
ln -s "${TMP_ROOT}/symlink-target" "${TMP_ROOT}/release-symlink"
expect_failure "existing symlink target rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "${TMP_ROOT}/release-symlink"
grep -q '^symlink target marker$' "${TMP_ROOT}/symlink-target/marker.txt" ||
  fail "symlink target changed after rejected create"
[[ ! -e "${TMP_ROOT}/symlink-target/source.tar.gz" ]] ||
  fail "rejected symlink target received release artifacts"

expect_failure "missing output parent rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "${TMP_ROOT}/missing-parent/release"
[[ ! -e "${TMP_ROOT}/missing-parent" ]] ||
  fail "create made a missing output parent"

expect_failure "dot basename rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "${TMP_ROOT}/."
expect_failure "dot-dot basename rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "${TMP_ROOT}/nested-parent/.."

if find "$TMP_ROOT" -maxdepth 1 -name '.*.staging.*' -print | grep -q .; then
  fail "release staging directory was not cleaned"
fi

printf 'dirty source\n' >>"${FIXTURE_REPO}/README.md"
expect_failure "dirty worktree rejection" \
  "$ASSEMBLER" create \
  --repo "$FIXTURE_REPO" \
  --revision HEAD \
  --output-dir "${TMP_ROOT}/dirty-release"
git -C "$FIXTURE_REPO" checkout -q -- README.md

cp "$ARCHIVE_ONE" "${TMP_ROOT}/source.tar.gz.clean"
printf 'tampered\n' >>"$ARCHIVE_ONE"
expect_failure "tampered archive rejection" \
  "$ASSEMBLER" verify --manifest "$MANIFEST_ONE"
cp "${TMP_ROOT}/source.tar.gz.clean" "$ARCHIVE_ONE"
"$ASSEMBLER" verify --manifest "$MANIFEST_ONE"

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
    "$ASSEMBLER" verify --manifest "$variant"
done

printf 'release manifest smoke: PASS\n'
