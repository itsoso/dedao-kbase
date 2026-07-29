#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSEMBLER="${SCRIPT_DIR}/assemble-release.sh"
PREPARER="${SCRIPT_DIR}/prepare-release.sh"
SCHEMA="dedao-kbase-prepared-release/v1"
MANIFEST_NAME="prepared-manifest.json"
SOURCE_ROOT_NAME="dedao-kbase-source"

usage() {
  cat <<'USAGE'
Usage:
  prepare-release.sh create --source-manifest PATH --output-dir PATH [tool options]
  prepare-release.sh verify --manifest PATH [--node-bin PATH]

Create tool options:
  --go-bin PATH
  --npm-bin PATH
  --node-bin PATH
  --tar-bin PATH
  --gzip-bin PATH
  --nginx-bin PATH
  --uname-bin PATH
  --proxy-smoke-script PATH
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
} else {
  throw new Error(`unsupported source manifest field: ${field}`);
}
NODE
}

write_prepared_manifest() {
  node_bin="$1"
  manifest="$2"
  revision="$3"
  bundle_dir="$4"
  "$node_bin" - "$manifest" "$revision" "$bundle_dir" "$SCHEMA" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const manifestPath = process.argv[2];
const revision = process.argv[3];
const bundleDirectory = process.argv[4];
const schema = process.argv[5];
const specifications = [
  {
    name: "kbase-server",
    path: "bundle/kbase-server",
    mode: "0755",
  },
  {
    name: "web",
    path: "bundle/web.tar.gz",
    mode: "0644",
  },
  {
    name: "nginx-template",
    path: "bundle/kbase.locations.conf.template",
    mode: "0644",
  },
  {
    name: "config-renderer",
    path: "bundle/render-kbase-config.sh",
    mode: "0755",
  },
];

function digest(filePath) {
  return crypto
    .createHash("sha256")
    .update(fs.readFileSync(filePath))
    .digest("hex");
}

const artifacts = specifications.map((specification) => ({
  name: specification.name,
  path: specification.path,
  sha256: digest(
    path.join(bundleDirectory, path.basename(specification.path)),
  ),
  mode: specification.mode,
}));
const manifest = { schema, revision, artifacts };
fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, {
  encoding: "utf8",
  mode: 0o644,
});
NODE
}

create_release() {
  source_manifest=""
  output_dir=""
  go_bin="go"
  npm_bin="npm"
  node_bin="node"
  tar_bin="tar"
  gzip_bin="gzip"
  nginx_bin="nginx"
  uname_bin="uname"
  proxy_smoke_script=""

  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --source-manifest)
        require_option_value "$1" "${2:-}"
        source_manifest="$2"
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
  [[ -n "$output_dir" ]] || fail "create requires --output-dir"
  [[ -x "$ASSEMBLER" ]] || fail "source release assembler is not executable"
  [[ -f "$source_manifest" ]] || fail "source manifest does not exist"

  # Source integrity is the first release gate. No build command may run first.
  "$ASSEMBLER" verify --manifest "$source_manifest"

  validate_output_target "$output_dir"
  require_executable "$node_bin" "Node"
  require_executable "$uname_bin" "uname"
  platform="$("$uname_bin" -s)"
  [[ "$platform" == "Linux" ]] ||
    fail "release preparation only supports Linux"

  require_executable "$go_bin" "Go"
  require_executable "$npm_bin" "npm"
  require_executable "$tar_bin" "tar"
  require_executable "$gzip_bin" "gzip"
  require_executable "$nginx_bin" "Nginx"

  revision="$(source_manifest_value "$node_bin" "$source_manifest" revision)"
  source_archive="$(source_manifest_value "$node_bin" "$source_manifest" artifact)"

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

  extract_dir="${temporary_dir}/source"
  bundle_dir="${temporary_dir}/bundle"
  mkdir -p "$extract_dir" "$bundle_dir"
  "$gzip_bin" -dc "$source_archive" |
    "$tar_bin" -xf - -C "$extract_dir"

  top_level_count="$(
    find "$extract_dir" -mindepth 1 -maxdepth 1 -print | wc -l |
      tr -d '[:space:]'
  )"
  source_root="${extract_dir}/${SOURCE_ROOT_NAME}"
  [[ "$top_level_count" == "1" && -d "$source_root" ]] ||
    fail "source archive must contain exactly ${SOURCE_ROOT_NAME}/"

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

  export LC_ALL=C
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
    CGO_ENABLED=1 "$go_bin" build -trimpath -o "$candidate" ./cmd/kbase-server
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
  "$PREPARER" verify \
    --node-bin "$node_bin" \
    --manifest "$prepared_manifest"

  rm -rf "$extract_dir"
  if [[ -e "$output_dir" || -L "$output_dir" ]]; then
    fail "output directory appeared during preparation: $output_dir"
  fi
  mv "$temporary_dir" "$output_dir"
  temporary_dir=""
  printf 'prepared release created: %s\n' "${output_dir}/${MANIFEST_NAME}"
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
  [[ -f "$manifest" && ! -L "$manifest" ]] ||
    fail "prepared manifest must be a regular file"

  "$node_bin" - "$manifest" "$SCHEMA" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const manifestPath = path.resolve(process.argv[2]);
const expectedSchema = process.argv[3];
const releaseDirectory = path.dirname(manifestPath);
const bundleDirectory = path.join(releaseDirectory, "bundle");
const expected = new Map([
  ["kbase-server", { path: "bundle/kbase-server", mode: "0755" }],
  ["web", { path: "bundle/web.tar.gz", mode: "0644" }],
  [
    "nginx-template",
    { path: "bundle/kbase.locations.conf.template", mode: "0644" },
  ],
  [
    "config-renderer",
    { path: "bundle/render-kbase-config.sh", mode: "0755" },
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
  const realArtifactPath = fs.realpathSync(artifactPath);
  const realReleasePrefix = `${realReleaseDirectory}${path.sep}`;
  if (!realArtifactPath.startsWith(realReleasePrefix)) {
    fail(`prepared artifact resolves outside release directory: ${artifact.name}`);
  }
  const actualMode = (stat.mode & 0o777).toString(8).padStart(4, "0");
  if (actualMode !== artifact.mode) {
    fail(`prepared artifact mode mismatch: ${artifact.name}`);
  }
  const actualDigest = crypto
    .createHash("sha256")
    .update(fs.readFileSync(artifactPath))
    .digest("hex");
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
