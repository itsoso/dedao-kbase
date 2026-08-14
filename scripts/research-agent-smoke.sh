#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

cd "$repo_root"

smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/research-agent-smoke.XXXXXX")"
fixture_pid=""
kbase_pid=""

cleanup() {
  status=$?
  if [[ -n "$kbase_pid" ]]; then
    kill "$kbase_pid" 2>/dev/null || true
    wait "$kbase_pid" 2>/dev/null || true
  fi
  if [[ -n "$fixture_pid" ]]; then
    kill "$fixture_pid" 2>/dev/null || true
    wait "$fixture_pid" 2>/dev/null || true
  fi
  if [[ "$status" -ne 0 ]]; then
    for log_file in "$smoke_root/kbase.log" "$smoke_root/worker.err" "$smoke_root/fixture.log"; do
      [[ -f "$log_file" ]] && sed -n '1,120p' "$log_file" >&2
    done
  fi
  rm -rf "$smoke_root"
  return "$status"
}
trap cleanup EXIT INT TERM

fixture_port_file="$smoke_root/fixture.port"
python3 scripts/research-agent-smoke-fixture.py "$fixture_port_file" >"$smoke_root/fixture.log" 2>&1 &
fixture_pid=$!
for _ in $(seq 1 100); do
  [[ -s "$fixture_port_file" ]] && break
  sleep 0.05
done
[[ -s "$fixture_port_file" ]]
fixture_port="$(tr -d '[:space:]' <"$fixture_port_file")"

kbase_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
kbase_url="http://127.0.0.1:$kbase_port"
fixture_url="http://127.0.0.1:$fixture_port"
store_root="$smoke_root/store"
state_root="$smoke_root/chatlog-state"
mkdir -p "$store_root" "$state_root"

go build -o "$smoke_root/kbase-server" ./cmd/kbase-server
go build -o "$smoke_root/chatlog-agent" ./cmd/chatlog-agent
go build -o "$smoke_root/source-agent-updater" ./cmd/source-agent-updater

smoke_token() {
  printf 'smoke-%s-%024d' "$1" 0
}
auth_token="$(smoke_token auth)"
source_token="$(smoke_token source)"
publisher_token="$(smoke_token publisher)"
retry_key="$(smoke_token retry)"

env \
  KBASE_AUTH_TOKEN="$auth_token" \
  KBASE_SOURCE_AGENT_TOKEN="$source_token" \
  KBASE_AGENT_PUBLISHER_TOKEN="$publisher_token" \
  KBASE_AUDIT_RETRY_SIGNING_KEY="$retry_key" \
  KBASE_BROWSER_SESSION_SECRET= \
  KBASE_SESSION_ADMIN_TOKEN= \
  KBASE_PUBLIC_ORIGIN= \
  KBASE_SOURCE_AGENT_ARTIFACT_ROOT= \
  KBASE_EVOLUTION_ENABLED=false \
  KBASE_RESEARCH_ENABLED=true \
  KBASE_RESEARCH_WORKERS=1 \
  KBASE_RESEARCH_POLL_MILLISECONDS=100 \
  KBASE_RESEARCH_PLANNER_MODEL=smoke-planner \
  KBASE_RESEARCH_EXTRACTOR_MODEL=smoke-extractor \
  KBASE_RESEARCH_SYNTHESIZER_MODEL=smoke-synthesizer \
  KBASE_RESEARCH_VERIFIER_MODEL=smoke-verifier \
  DEDAO_TOKENPLAN_API_KEY=synthetic-smoke-key \
  DEDAO_TOKENPLAN_BASE_URL="$fixture_url/v1" \
  "$smoke_root/kbase-server" --addr "127.0.0.1:$kbase_port" --root "$store_root" --evolution-enabled=false \
  >"$smoke_root/kbase.log" 2>&1 &
kbase_pid=$!

for _ in $(seq 1 200); do
  if curl --fail --silent "$kbase_url/health" >/dev/null; then
    break
  fi
  if ! kill -0 "$kbase_pid" 2>/dev/null; then
    sed -n '1,120p' "$smoke_root/kbase.log" >&2
    exit 1
  fi
  sleep 0.05
done
curl --fail --silent "$kbase_url/health" >/dev/null

run_response="$smoke_root/run.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: full-stack-smoke' \
  --data '{"mode":"auto","question":"Compare the synthetic history timeline and conflict.","requested_sources":["chatlog"],"subject_ids":["smoke-subject"]}' \
  "$kbase_url/api/research/runs" >"$run_response"
run_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["run"]["run_id"])' "$run_response")"

worker_output="$smoke_root/worker.json"
for _ in $(seq 1 100); do
  env \
    KBASE_REMOTE_URL="$kbase_url" \
    KBASE_SOURCE_AGENT_TOKEN="$source_token" \
    KBASE_SOURCE_AGENT_ID=chatlog-agent \
    CHATLOG_AGENT_STATE_DIR="$state_root" \
    CHATLOG_BASE_URL="$fixture_url" \
    "$smoke_root/chatlog-agent" once >"$worker_output" 2>"$smoke_root/worker.err"
  if python3 -c 'import json,sys; raise SystemExit(0 if json.load(open(sys.argv[1])).get("job_id") else 1)' "$worker_output"; then
    break
  fi
  sleep 0.1
done
python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); assert value.get("ok") and value.get("heartbeat") and value.get("job_state")=="completed" and value.get("evidence_count")==1' "$worker_output"

detail_response="$smoke_root/detail.json"
for _ in $(seq 1 300); do
  curl --fail --silent --show-error -H "Authorization: Bearer $auth_token" \
    "$kbase_url/api/research/runs/$run_id" >"$detail_response"
  status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["run"]["status"])' "$detail_response")"
  [[ "$status" == "completed" ]] && break
  [[ "$status" == "failed" || "$status" == "insufficient" || "$status" == "canceled" ]] && exit 1
  sleep 0.1
done
python3 -c 'import json,sys; run=json.load(open(sys.argv[1]))["run"]; assert run["status"]=="completed"; assert run["actual_scope"]["searched_sources"]==["chatlog"]; assert run["actual_scope"]["cited_sources"]==["chatlog"]' "$detail_response"

events_response="$smoke_root/events.json"
curl --fail --silent --show-error -H "Authorization: Bearer $auth_token" \
  "$kbase_url/api/research/runs/$run_id/events?after=0" >"$events_response"
python3 -c 'import json,sys; codes={item["code"] for item in json.load(open(sys.argv[1]))["events"]}; required={"worker_job_queued","worker_job_completed","completed"}; assert required <= codes, (required-codes)' "$events_response"

python3 - "$store_root/research_control.sqlite3" "$run_id" <<'PY'
import json
import sqlite3
import sys

database, run_id = sys.argv[1:]
connection = sqlite3.connect(database)
conclusions = connection.execute(
    "SELECT evidence_ids_json FROM research_conclusions WHERE run_id = ?", (run_id,)
).fetchall()
jobs = connection.execute(
    "SELECT COUNT(*) FROM research_worker_jobs WHERE run_id = ? AND state = 'completed'", (run_id,)
).fetchone()[0]
events = connection.execute(
    "SELECT COUNT(*) FROM research_events WHERE run_id = ?", (run_id,)
).fetchone()[0]
assert len(conclusions) == 1 and json.loads(conclusions[0][0])
assert jobs == 1 and events >= 8
PY

if rg -q 'SMOKE_PRIVATE_RAW_SENTINEL' "$smoke_root/kbase.log" "$smoke_root/worker.json" "$smoke_root/worker.err"; then
  printf '%s\n' 'private sentinel leaked to process output' >&2
  exit 1
fi

go test ./backend/app -count=1 -run 'TestResearchOrchestrator(DeepPathWaitsResumesAndSurvivesRestart|TypedOutcomesAndCancellation|CoordinatorAdvancesDurableRunsAndShutsDown)|TestKBaseHTTPResearch(WorkerAuthenticationAndValidation|RunLifecycleBearerCompatibilityAndRedaction)|TestResearchIdentityExactBindingWinsAndNameSimilarityAloneRemainsAmbiguous'

go test ./cmd/chatlog-agent -count=1 -run 'TestChatlogAgentOnceHeartbeatsAndCompletesSearchWithoutLoggingContent|TestChatlogAgentReportsDependencyUnavailableWithoutLeakingDetails'

go test ./cmd/kbase-server -count=1 -run 'TestResearchServerRuntimeStartsRecoversAndShutsDown|TestResearchServerConfigurationUsesStrictBoundedEnvironment'

go test ./backend/app -count=1 -run 'TestResearchEvaluationSyntheticGoldPassesAllHardGates|TestResearchEvaluationHardFailuresCannotBeOverriddenByAggregateScore|TestKBaseHTTPHandlerTrustsEvaluatesAndPublishesResearchPackageV3'

printf '%s\n' 'research agent smoke passed'
