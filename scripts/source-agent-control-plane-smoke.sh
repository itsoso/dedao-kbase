#!/usr/bin/env bash

set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/source-agent-control-plane.XXXXXX")"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

catalog_fixture="$root/backend/app/testdata/source-agent-artifacts/catalog.json"
wechat_fixture="$root/backend/app/testdata/source-agent-protocol/wechat-heartbeat.json"
wcplus_fixture="$root/backend/app/testdata/source-agent-protocol/wcplus-heartbeat.json"
for fixture in "$catalog_fixture" "$wechat_fixture" "$wcplus_fixture"; do
  [[ -f "$fixture" ]] || { echo "missing control-plane fixture: ${fixture#"$root/"}" >&2; exit 1; }
done

if grep -Eiq '(bearer|authorization|cookie|token|/Users/|/home/|source_body|article_body)' \
  "$catalog_fixture" "$wechat_fixture" "$wcplus_fixture"; then
  echo "source agent fixture contains private or credential-shaped data" >&2
  exit 1
fi

artifact_root="$tmp_dir/artifacts"
mkdir -p "$artifact_root/artifacts"
printf 'fixture worker v1\n' >"$artifact_root/artifacts/fixture-worker"
cp "$catalog_fixture" "$artifact_root/catalog.json"

expected_artifact_sha="b781fb5d8acb3529152231ee8d9932ffc074dbdbe9521ca869cd258ebe1671ec"
sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d ' ' -f 1
  else
    shasum -a 256 "$1" | cut -d ' ' -f 1
  fi
}
actual_artifact_sha="$(sha256_file "$artifact_root/artifacts/fixture-worker")"
[[ "$actual_artifact_sha" == "$expected_artifact_sha" ]] || { echo "generated artifact hash mismatch" >&2; exit 1; }
cp "$artifact_root/artifacts/fixture-worker" "$tmp_dir/promoted-worker"
promoted_sha="$(sha256_file "$tmp_dir/promoted-worker")"
[[ "$promoted_sha" == "$actual_artifact_sha" ]] || { echo "staging and production artifact hashes differ" >&2; exit 1; }

admin_token="admin-$(openssl rand -hex 24)"
source_token="source-$(openssl rand -hex 24)"
publisher_token="publisher-$(openssl rand -hex 24)"
port=$((20000 + RANDOM % 20000))
server_bin="$tmp_dir/kbase-server"
(
  cd "$root"
  go build -trimpath -o "$server_bin" ./cmd/kbase-server
)
KBASE_SOURCE_AGENT_ARTIFACT_ROOT="$artifact_root" \
"$server_bin" \
  --addr "127.0.0.1:$port" \
  --root "$tmp_dir/store" \
  --system-kb-export "$tmp_dir/system-kb.json" \
  --web-dir "$root/frontend-web" \
  --auth-token "$admin_token" \
  --source-agent-token "$source_token" \
  --agent-publisher-token "$publisher_token" \
  >"$tmp_dir/server.log" 2>&1 &
server_pid=$!

for _ in $(seq 1 80); do
  if curl --fail --silent "http://127.0.0.1:$port/health" >/dev/null; then break; fi
  sleep 0.1
done
curl --fail --silent --show-error "http://127.0.0.1:$port/health" >/dev/null

artifacts_json="$(curl --fail --silent --show-error -H "Authorization: Bearer $admin_token" "http://127.0.0.1:$port/api/source-agent-artifacts")"
grep -Fq '"id":"fixture-wechat-2"' <<<"$artifacts_json" || { echo "fixture artifact is absent from catalog response" >&2; exit 1; }
grep -Fq '"sha256":"b781fb5d8acb3529152231ee8d9932ffc074dbdbe9521ca869cd258ebe1671ec"' <<<"$artifacts_json" || { echo "fixture artifact hash is absent from catalog response" >&2; exit 1; }

for fixture in "$wechat_fixture" "$wcplus_fixture"; do
  curl --fail --silent --show-error \
    -H "Authorization: Bearer $source_token" \
    -H 'Content-Type: application/json' \
    --data-binary "@$fixture" \
    "http://127.0.0.1:$port/api/source-agent/heartbeat" >/dev/null
done

agents_json="$(curl --fail --silent --show-error -H "Authorization: Bearer $admin_token" "http://127.0.0.1:$port/api/source-agents")"
grep -Fq '"agent_id":"fixture-wechat-worker"' <<<"$agents_json" || { echo "WeChat fixture worker was not registered" >&2; exit 1; }
grep -Fq '"agent_id":"fixture-wcplus-worker"' <<<"$agents_json" || { echo "WCPlus fixture worker was not registered" >&2; exit 1; }
grep -Fq '"vendor_blocked"' <<<"$agents_json" || { echo "WCPlus vendor-blocked state was not preserved" >&2; exit 1; }

curl --fail --silent --show-error \
  -H "Authorization: Bearer $admin_token" \
  -H 'Content-Type: application/json' \
  --data '{"desired_state":"paused"}' \
  "http://127.0.0.1:$port/api/source-agents/fixture-wechat-worker/desired-state" \
  | grep -Fq '"desired_state":"paused"'

wcplus_json="$(curl --fail --silent --show-error -H "Authorization: Bearer $admin_token" "http://127.0.0.1:$port/api/source-agents/fixture-wcplus-worker")"
grep -Fq '"desired_state":"active"' <<<"$wcplus_json" || { echo "pausing WeChat affected WCPlus desired state" >&2; exit 1; }
grep -Fq '"vendor_blocked"' <<<"$wcplus_json" || { echo "WCPlus detail lost vendor-blocked state" >&2; exit 1; }

expires_at="$(date -u -v+10M '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '+10 minutes' '+%Y-%m-%dT%H:%M:%SZ')"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $admin_token" \
  -H 'Content-Type: application/json' \
  --data "{\"type\":\"diagnose\",\"idempotency_key\":\"fixture-diagnose\",\"expires_at\":\"$expires_at\"}" \
  "http://127.0.0.1:$port/api/source-agents/fixture-wcplus-worker/commands" \
  | grep -Fq '"type":"diagnose"'

(
  cd "$root"
  go test ./backend/app -run 'Test(SourceLeaseRejectsPausedAgent|SourceAgentCommandRunnerRespectsPausedAndUnhealthyHeartbeat|SourceAgentCommandDiagnoseLifecycleAndClaimNext|SourceAgentArtifactCatalogValidatesAndReadsExactBytes|SourceAgentArtifactCatalogReloadsRolloutKillSwitch|KBaseHTTPHandlerSourceAgentArtifactDownloadIsCommandBound|KBaseHTTPHandlerSourceAgentArtifactRolloutGate)' -count=1
  go test ./internal/sourceagentupdate -count=1
  go test ./cmd/wcplus-agent -run 'TestWCPlusAgentBlockedCapabilityOnceDoesNotReportSuccess' -count=1
  if [[ "$(uname -s)" == "Darwin" ]]; then
    go test ./backend/app -run 'TestSourceAgentUpdateBridgeRetryAndCrashBoundaries' -count=1
  fi
)

echo "source agent control plane smoke passed"
