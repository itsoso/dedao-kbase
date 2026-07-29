#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSEMBLER="${SCRIPT_DIR}/assemble-release.sh"
SCHEMA="dedao-kbase-source-release/v1"
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

if [[ ! -x "$ASSEMBLER" ]]; then
  fail "assembler is missing or not executable: ${ASSEMBLER}"
fi

command -v git >/dev/null 2>&1 || fail "git is required"
command -v node >/dev/null 2>&1 || fail "node is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kbase-release-manifest.XXXXXX")"
trap 'rm -rf "$TMP_ROOT"' EXIT

FIXTURE_REPO="${TMP_ROOT}/fixture"
OUTPUT_ONE="${TMP_ROOT}/release-one"
OUTPUT_TWO="${TMP_ROOT}/release-two"
mkdir -p "$FIXTURE_REPO"

git -C "$FIXTURE_REPO" init -q
git -C "$FIXTURE_REPO" config user.name "Release Smoke"
git -C "$FIXTURE_REPO" config user.email "release-smoke@example.invalid"
printf 'fixture source\n' >"${FIXTURE_REPO}/README.md"
mkdir -p "${FIXTURE_REPO}/nested"
printf 'package fixture\n' >"${FIXTURE_REPO}/nested/package.txt"
git -C "$FIXTURE_REPO" add README.md nested/package.txt
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
