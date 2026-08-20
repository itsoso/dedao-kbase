#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"

cd "$repo_root"

smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/research-agent-smoke.XXXXXX")"
fixture_pid=""
kbase_pid=""

emit_failure_diagnostics() {
  local status="$1"
  local log_file=""
  local file_name=""
  local byte_count=""
  local digest=""

  printf 'research agent smoke failed: exit_status=%d\n' "$status" >&2
  for log_file in "$smoke_root/kbase.log" "$smoke_root/worker.err" "$smoke_root/fixture.log"; do
    [[ -f "$log_file" ]] || continue
    file_name="$(basename "$log_file")"
    byte_count="$(wc -c <"$log_file" | tr -d '[:space:]')"
    digest="$(python3 - "$log_file" 2>/dev/null <<'PY'
import hashlib
import sys

with open(sys.argv[1], "rb") as source:
    print(hashlib.sha256(source.read()).hexdigest())
PY
)" || digest="$(printf '%064d' 0)"
    printf 'diagnostic file=%s bytes=%s sha256=%s\n' "$file_name" "$byte_count" "$digest" >&2
  done
}

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
    emit_failure_diagnostics "$status"
  fi
  rm -rf "$smoke_root"
  return "$status"
}
trap cleanup EXIT INT TERM

if [[ "${RESEARCH_AGENT_SMOKE_FAILURE_PRIVACY_SELF_TEST:-}" == "1" ]]; then
  privacy_payload="${RESEARCH_AGENT_SMOKE_FAILURE_PRIVACY_PAYLOAD:-}"
  printf '%s' "$privacy_payload" >"$smoke_root/kbase.log"
  printf '%s' "$privacy_payload" >"$smoke_root/worker.err"
  printf '%s' "$privacy_payload" >"$smoke_root/fixture.log"
  exit 97
fi

if [[ "${RESEARCH_AGENT_SMOKE_SKIP_STANDARD_SELF_TESTS:-}" != "1" ]]; then
  env RESEARCH_AGENT_SMOKE_SKIP_STANDARD_SELF_TESTS=1 \
    python3 scripts/research-agent-smoke-binding-test.py
  env RESEARCH_AGENT_SMOKE_SKIP_STANDARD_SELF_TESTS=1 \
    bash scripts/research-agent-smoke-privacy-test.sh
fi

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

research_package_output="$smoke_root/research-package.txt"
go run ./scripts/research-smoke-seed \
  "$store_root" \
  backend/app/testdata/research-evaluation-v1.synthetic.json \
  >"$research_package_output"
[[ "$(wc -l <"$research_package_output" | tr -d '[:space:]')" == "3" ]]
research_package_id="$(sed -n '1p' "$research_package_output")"
research_package_version="$(sed -n '2p' "$research_package_output")"
collection_release_id="$(sed -n '3p' "$research_package_output")"
[[ -n "$research_package_id" && -n "$research_package_version" && -n "$collection_release_id" ]]

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
    exit 1
  fi
  sleep 0.05
done
curl --fail --silent "$kbase_url/health" >/dev/null

materialization_response="$smoke_root/materialization.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data '{}' \
  "$kbase_url/api/knowledge/collection-releases/$collection_release_id/materialize" \
  >"$materialization_response"
materialized_release_id="$(python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); assert value["created"] is True; print(value["release"]["release_id"])' "$materialization_response")"

collection_compile_response="$smoke_root/collection-compile.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data "{\"schema_version\":\"agent-compilation-request.v1\",\"mode\":\"study\",\"primary_release_id\":\"$materialized_release_id\",\"version\":\"1.0.0\",\"research_enabled\":true}" \
  "$kbase_url/api/agent-packages/compile" >"$collection_compile_response"

trusted_request="$smoke_root/collection-trusted-request.json"
evaluation_request="$smoke_root/collection-evaluation-request.json"
publish_request="$smoke_root/collection-publish-request.json"
collection_package_meta="$smoke_root/collection-package-meta.txt"
python3 - "$collection_compile_response" backend/app/testdata/research-evaluation-v1.synthetic.json \
  "$trusted_request" "$evaluation_request" "$publish_request" "$collection_package_meta" <<'PY'
import copy
import json
import sys

compile_path, suite_path, trust_path, evaluation_path, publish_path, meta_path = sys.argv[1:]
compilation = json.load(open(compile_path, encoding="utf-8"))
candidate = compilation["candidates"][0]
assert candidate["status"] == "ready"
package = candidate["package"]
assert package["schema_version"] == "agent-package.v4"
assert package.get("collection_releases") in (None, [])
submitted = json.load(open(suite_path, encoding="utf-8"))
trusted = copy.deepcopy(submitted)
for case in trusted.get("research_cases", []):
    case["observed"] = {}
json.dump({"package": package, "suite": trusted}, open(trust_path, "w", encoding="utf-8"), separators=(",", ":"))
json.dump({"package": package, "suite": submitted}, open(evaluation_path, "w", encoding="utf-8"), separators=(",", ":"))
json.dump({"idempotency_key": "materialized-collection-smoke", "package": package}, open(publish_path, "w", encoding="utf-8"), separators=(",", ":"))
with open(meta_path, "w", encoding="utf-8") as output:
    output.write(package["package_id"] + "\n" + package["version"] + "\n")
PY
collection_package_id="$(sed -n '1p' "$collection_package_meta")"
collection_package_version="$(sed -n '2p' "$collection_package_meta")"

curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" -H 'Content-Type: application/json' \
  --data-binary "@$trusted_request" \
  "$kbase_url/api/agent-packages/evaluation-suites/trust" >"$smoke_root/collection-trusted.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $publisher_token" -H 'Content-Type: application/json' \
  --data-binary "@$evaluation_request" \
  "$kbase_url/api/agent-packages/evaluate" >"$smoke_root/collection-evaluated.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $publisher_token" -H 'Content-Type: application/json' \
  --data-binary "@$publish_request" \
  "$kbase_url/api/agent-packages/publish" >"$smoke_root/collection-published.json"
python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); assert value["package"]["schema_version"]=="agent-package.v4"' "$smoke_root/collection-published.json"

quick_preflight_response="$smoke_root/quick-preflight.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data '{"mode":"quick","question":"What does the synthetic collection evidence support?","requested_sources":["knowledge"]}' \
  "$kbase_url/api/research/preflight" >"$quick_preflight_response"
quick_preflight_id="$(python3 - "$quick_preflight_response" "$collection_package_id" "$collection_package_version" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
expected = (sys.argv[2], sys.argv[3])
candidates = value["candidates"]
assert value["status"] == "ready"
assert 0 < len(candidates) <= 3
assert (candidates[0]["package_id"], candidates[0]["package_version"]) == expected
assert candidates[0]["readiness"] in ("pass", "warning")
worker = next(item for item in value["checks"] if item["code"] == "worker")
assert worker["status"] == "pass" and worker["result_code"] == "not_required"
print(value["preflight_id"])
PY
)"

quick_response="$smoke_root/quick-run.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: materialized-collection-quick-smoke' \
  --data "{\"preflight_id\":\"$quick_preflight_id\",\"mode\":\"quick\",\"question\":\"What does the synthetic collection evidence support?\",\"package_id\":\"$collection_package_id\",\"package_version\":\"$collection_package_version\",\"requested_sources\":[\"knowledge\"]}" \
  "$kbase_url/api/research/runs" >"$quick_response"
quick_run_id="$(python3 scripts/research_agent_smoke_assertions.py create \
  "$quick_response" "$quick_preflight_id" "$collection_package_id" "$collection_package_version" quick)"
quick_detail="$smoke_root/quick-detail.json"
for _ in $(seq 1 300); do
  curl --fail --silent --show-error -H "Authorization: Bearer $auth_token" \
    "$kbase_url/api/research/runs/$quick_run_id" >"$quick_detail"
  quick_status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["run"]["status"])' "$quick_detail")"
  [[ "$quick_status" == "completed" ]] && break
  [[ "$quick_status" == "failed" || "$quick_status" == "insufficient" || "$quick_status" == "canceled" ]] && exit 1
  sleep 0.1
done
python3 scripts/research_agent_smoke_assertions.py detail \
  "$quick_detail" "$quick_preflight_id" "$collection_package_id" "$collection_package_version" quick
python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); run=value["run"]; assert run["actual_scope"]["searched_sources"]==["knowledge"]; assert run["actual_scope"]["cited_sources"]==["knowledge"]; assert len(value["evidence"])>=1; assert value["conclusions"][0]["citations"][0]["available"] is True' "$quick_detail"
quick_citation_href="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["conclusions"][0]["citations"][0]["href"])' "$quick_detail")"
curl --fail --silent --show-error -H "Authorization: Bearer $auth_token" \
  "$kbase_url$quick_citation_href" >"$smoke_root/quick-citation.json"
python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); assert value["citation"]["citation_id"] and value["citation"]["chunk_id"]; assert value["claim_ids"]' "$smoke_root/quick-citation.json"
python3 - "$store_root/research_control.sqlite3" "$quick_run_id" <<'PY'
import sqlite3
import sys

database, run_id = sys.argv[1:]
connection = sqlite3.connect(database)
tools = [row[0] for row in connection.execute(
    "SELECT tool_name FROM research_tool_audits WHERE run_id = ? ORDER BY created_at", (run_id,)
)]
assert "search_knowledge" in tools and "fetch_knowledge_evidence" in tools, tools
PY

linked_preflight_response="$smoke_root/linked-preflight.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data "{\"mode\":\"quick\",\"question\":\"What follow-up does the synthetic collection evidence support?\",\"requested_sources\":[\"knowledge\"],\"package_constraint\":\"$collection_package_id\",\"parent_run_id\":\"$quick_run_id\"}" \
  "$kbase_url/api/research/preflight" >"$linked_preflight_response"
linked_preflight_id="$(python3 -c 'import json,sys; value=json.load(open(sys.argv[1], encoding="utf-8")); assert value["status"]=="ready" and value["parent_run_id"]==sys.argv[2]; print(value["preflight_id"])' "$linked_preflight_response" "$quick_run_id")"
linked_response="$smoke_root/linked-run.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: materialized-collection-linked-smoke' \
  --data "{\"preflight_id\":\"$linked_preflight_id\",\"mode\":\"quick\",\"question\":\"What follow-up does the synthetic collection evidence support?\",\"package_id\":\"$collection_package_id\",\"package_version\":\"$collection_package_version\",\"requested_sources\":[\"knowledge\"]}" \
  "$kbase_url/api/research/runs" >"$linked_response"
linked_run_id="$(python3 -c 'import json,sys; run=json.load(open(sys.argv[1], encoding="utf-8"))["run"]; assert run["parent_run_id"]==sys.argv[2]; print(run["run_id"])' "$linked_response" "$quick_run_id")"
python3 - "$store_root/research_control.sqlite3" "$linked_run_id" "$quick_run_id" <<'PY'
import sqlite3
import sys

database, run_id, parent_run_id = sys.argv[1:]
connection = sqlite3.connect(database)
stored_parent = connection.execute(
    "SELECT parent_run_id FROM research_runs WHERE run_id = ?", (run_id,)
).fetchone()
assert stored_parent == (parent_run_id,)
PY

rejected_run_count="$(python3 - "$store_root/research_control.sqlite3" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
print(connection.execute("SELECT COUNT(*) FROM research_runs").fetchone()[0])
PY
)"

expired_preflight_response="$smoke_root/expired-preflight.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data "{\"mode\":\"quick\",\"question\":\"What expired synthetic evidence is available?\",\"requested_sources\":[\"knowledge\"],\"package_constraint\":\"$collection_package_id\"}" \
  "$kbase_url/api/research/preflight" >"$expired_preflight_response"
expired_preflight_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["preflight_id"])' "$expired_preflight_response")"
python3 - "$store_root/research_control.sqlite3" "$expired_preflight_id" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
connection.execute(
    "UPDATE research_preflights SET created_at = ?, expires_at = ? WHERE preflight_id = ?",
    ("2000-01-01T00:00:00.000000000Z", "2000-01-01T00:10:00.000000000Z", sys.argv[2]),
)
connection.commit()
PY
expired_status="$(curl --silent --show-error --output "$smoke_root/expired-run-error.json" --write-out '%{http_code}' \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: expired-preflight-smoke' \
  --data "{\"preflight_id\":\"$expired_preflight_id\",\"mode\":\"quick\",\"question\":\"What expired synthetic evidence is available?\",\"package_id\":\"$collection_package_id\",\"package_version\":\"$collection_package_version\",\"requested_sources\":[\"knowledge\"]}" \
  "$kbase_url/api/research/runs")"
[[ "$expired_status" == "410" ]]
python3 -c 'import json,sys; assert json.load(open(sys.argv[1], encoding="utf-8")) == {"error":"preflight_expired"}' "$smoke_root/expired-run-error.json"

foreign_preflight_response="$smoke_root/foreign-preflight.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data "{\"mode\":\"quick\",\"question\":\"What owner-bound synthetic evidence is available?\",\"requested_sources\":[\"knowledge\"],\"package_constraint\":\"$collection_package_id\"}" \
  "$kbase_url/api/research/preflight" >"$foreign_preflight_response"
foreign_preflight_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["preflight_id"])' "$foreign_preflight_response")"
python3 - "$store_root/research_control.sqlite3" "$foreign_preflight_id" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
connection.execute(
    "UPDATE research_preflights SET owner_hash = ? WHERE preflight_id = ?",
    ("b" * 64, sys.argv[2]),
)
connection.commit()
PY
foreign_status="$(curl --silent --show-error --output "$smoke_root/foreign-run-error.json" --write-out '%{http_code}' \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: foreign-preflight-smoke' \
  --data "{\"preflight_id\":\"$foreign_preflight_id\",\"mode\":\"quick\",\"question\":\"What owner-bound synthetic evidence is available?\",\"package_id\":\"$collection_package_id\",\"package_version\":\"$collection_package_version\",\"requested_sources\":[\"knowledge\"]}" \
  "$kbase_url/api/research/runs")"
[[ "$foreign_status" == "404" ]]
python3 -c 'import json,sys; assert json.load(open(sys.argv[1], encoding="utf-8")) == {"error":"research_preflight_not_found"}' "$smoke_root/foreign-run-error.json"
python3 - "$store_root/research_control.sqlite3" "$rejected_run_count" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
assert connection.execute("SELECT COUNT(*) FROM research_runs").fetchone()[0] == int(sys.argv[2])
PY

blocked_preflight_response="$smoke_root/blocked-preflight.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data "{\"mode\":\"auto\",\"question\":\"Compare the synthetic history timeline and conflict.\",\"requested_sources\":[\"chatlog\"],\"subject_ids\":[\"smoke-subject\"],\"package_constraint\":\"$research_package_id\"}" \
  "$kbase_url/api/research/preflight" >"$blocked_preflight_response"
python3 - "$blocked_preflight_response" "$research_package_id" "$research_package_version" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
expected = (sys.argv[2], sys.argv[3])
assert value["status"] == "blocked"
assert 0 < len(value["candidates"]) <= 3
assert (value["candidates"][0]["package_id"], value["candidates"][0]["package_version"]) == expected
worker = next(item for item in value["checks"] if item["code"] == "worker")
assert worker["status"] == "blocked" and worker["result_code"] != "not_required"
PY

env \
  KBASE_REMOTE_URL="$kbase_url" \
  KBASE_SOURCE_AGENT_TOKEN="$source_token" \
  KBASE_SOURCE_AGENT_ID=chatlog-agent \
  CHATLOG_AGENT_STATE_DIR="$state_root" \
  CHATLOG_BASE_URL="$fixture_url" \
  "$smoke_root/chatlog-agent" once >"$smoke_root/worker-heartbeat.json" 2>"$smoke_root/worker.err"

deep_preflight_response="$smoke_root/deep-preflight.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  --data "{\"mode\":\"auto\",\"question\":\"Compare the synthetic history timeline and conflict.\",\"requested_sources\":[\"chatlog\"],\"subject_ids\":[\"smoke-subject\"],\"package_constraint\":\"$research_package_id\"}" \
  "$kbase_url/api/research/preflight" >"$deep_preflight_response"
deep_preflight_id="$(python3 - "$deep_preflight_response" "$research_package_id" "$research_package_version" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
expected = (sys.argv[2], sys.argv[3])
candidates = value["candidates"]
assert value["status"] == "ready"
assert 0 < len(candidates) <= 3
assert (candidates[0]["package_id"], candidates[0]["package_version"]) == expected
assert candidates[0]["readiness"] in ("pass", "warning")
worker = next(item for item in value["checks"] if item["code"] == "worker")
assert worker["status"] == "pass" and worker["result_code"] == "online"
print(value["preflight_id"])
PY
)"

run_response="$smoke_root/run.json"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $auth_token" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: full-stack-smoke' \
  --data "{\"preflight_id\":\"$deep_preflight_id\",\"mode\":\"auto\",\"question\":\"Compare the synthetic history timeline and conflict.\",\"package_id\":\"$research_package_id\",\"package_version\":\"$research_package_version\",\"requested_sources\":[\"chatlog\"],\"subject_ids\":[\"smoke-subject\"]}" \
  "$kbase_url/api/research/runs" >"$run_response"
run_id="$(python3 scripts/research_agent_smoke_assertions.py create \
  "$run_response" "$deep_preflight_id" "$research_package_id" "$research_package_version" deep)"

worker_output="$smoke_root/worker.json"
detail_response="$smoke_root/detail.json"
worker_job_completed=false
for _ in $(seq 1 300); do
  env \
    KBASE_REMOTE_URL="$kbase_url" \
    KBASE_SOURCE_AGENT_TOKEN="$source_token" \
    KBASE_SOURCE_AGENT_ID=chatlog-agent \
    CHATLOG_AGENT_STATE_DIR="$state_root" \
    CHATLOG_BASE_URL="$fixture_url" \
    "$smoke_root/chatlog-agent" once >"$worker_output" 2>"$smoke_root/worker.err"
  if python3 -c 'import json,sys; value=json.load(open(sys.argv[1])); raise SystemExit(0 if value.get("job_state")=="completed" else 1)' "$worker_output"; then
    worker_job_completed=true
  fi
  curl --fail --silent --show-error -H "Authorization: Bearer $auth_token" \
    "$kbase_url/api/research/runs/$run_id" >"$detail_response"
  status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["run"]["status"])' "$detail_response")"
  [[ "$status" == "completed" ]] && break
  [[ "$status" == "failed" || "$status" == "insufficient" || "$status" == "canceled" ]] && exit 1
  sleep 0.1
done
[[ "$worker_job_completed" == "true" ]]
python3 scripts/research_agent_smoke_assertions.py detail \
  "$detail_response" "$deep_preflight_id" "$research_package_id" "$research_package_version" deep
python3 -c 'import json,sys; run=json.load(open(sys.argv[1]))["run"]; assert run["actual_scope"]["searched_sources"]==["chatlog"]; assert run["actual_scope"]["cited_sources"]==["chatlog"]' "$detail_response"

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
expand_jobs = connection.execute(
    "SELECT COUNT(*) FROM research_worker_jobs WHERE run_id = ? AND tool = 'expand_chat_context' AND state = 'completed'", (run_id,)
).fetchone()[0]
evidence = connection.execute(
    "SELECT COUNT(*) FROM research_evidence WHERE run_id = ?", (run_id,)
).fetchone()[0]
events = connection.execute(
    "SELECT COUNT(*) FROM research_events WHERE run_id = ?", (run_id,)
).fetchone()[0]
assert len(conclusions) == 1 and json.loads(conclusions[0][0])
assert jobs == 3 and expand_jobs == 1 and evidence == 3 and events >= 12
PY

if rg -q 'SMOKE_PRIVATE_RAW_SENTINEL' "$smoke_root/kbase.log" "$smoke_root/worker.json" "$smoke_root/worker.err"; then
  printf '%s\n' 'private sentinel leaked to process output' >&2
  exit 1
fi

go test ./backend/app -count=1 -run 'TestResearchOrchestrator(DeepPathWaitsResumesAndSurvivesRestart|TypedOutcomesAndCancellation|CoordinatorAdvancesDurableRunsAndShutsDown)|TestKBaseHTTPResearch(WorkerAuthenticationAndValidation|RunLifecycleBearerCompatibilityAndRedaction)|TestResearchIdentityExactBindingWinsAndNameSimilarityAloneRemainsAmbiguous'

go test ./cmd/chatlog-agent -count=1 -run 'TestChatlogAgentOnceHeartbeatsAndCompletesSearchWithoutLoggingContent|TestChatlogAgentReportsDependencyUnavailableWithoutLeakingDetails'

go test ./cmd/kbase-server -count=1 -run 'TestResearchServerRuntimeStartsRecoversAndShutsDown|TestResearchServerConfigurationUsesStrictBoundedEnvironment'

go test ./backend/app -count=1 -run 'TestResearchEvaluationSyntheticGoldPassesAllHardGates|TestResearchEvaluationHardFailuresCannotBeOverriddenByAggregateScore|TestKBaseHTTPHandlerTrustsEvaluatesAndPublishesResearchPackageV4'

printf '%s\n' 'research agent smoke passed'
