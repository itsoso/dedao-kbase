#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/source-agent-artifact-smoke.XXXXXX")"
trap 'rm -rf "$fixture_root"' EXIT

artifact_dir="$fixture_root/artifacts"
artifact_path="$artifact_dir/worker"
catalog_path="$fixture_root/catalog.json"
mkdir -p "$artifact_dir"
printf '%s' 'source-agent-artifact-smoke' >"$artifact_path"

revision="$(git -C "$repo_root" rev-parse HEAD)"
size="$(wc -c <"$artifact_path")"
read -r sha256 _ < <(shasum -a 256 "$artifact_path")

write_catalog() {
  local storage_key="$1"
  printf '{"artifacts":[{"id":"smoke-artifact","worker_type":"wechat-worker","platform":"darwin","architecture":"arm64","revision":"%s","version":"2.0.0","protocol_version":"2026-08-01","minimum_version":"1.0.0","channel":"staging","release_notes":"Operator approved maintenance release","size":%s,"sha256":"%s","storage_key":"%s","build_gate":"passed","allowed_for_rollout":true}]}\n' \
    "$revision" "$size" "$sha256" "$storage_key" >"$catalog_path"
}

run_fixture() {
  KBASE_ARTIFACT_SMOKE_ROOT="$fixture_root" \
    KBASE_ARTIFACT_SMOKE_REVISION="$revision" \
    go test "$repo_root/backend/app" -run '^TestSourceAgentArtifactPackagingSmokeFixture$' -count=1
}

write_catalog 'artifacts/worker'
run_fixture

printf '%s' '-altered' >>"$artifact_path"
if run_fixture >"$fixture_root/altered.log" 2>&1; then
  printf '%s\n' 'altered artifact unexpectedly passed validation' >&2
  exit 1
fi

printf '%s' 'source-agent-artifact-smoke' >"$artifact_path"
write_catalog '../worker'
if run_fixture >"$fixture_root/traversal.log" 2>&1; then
  printf '%s\n' 'traversal storage key unexpectedly passed validation' >&2
  exit 1
fi

printf '%s\n' 'source agent artifact smoke: PASS'
