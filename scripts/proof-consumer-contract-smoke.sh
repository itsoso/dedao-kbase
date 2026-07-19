#!/usr/bin/env bash
set -euo pipefail

python3 - <<'PY'
import json
from pathlib import Path

path = Path("testdata/agent-evals/proof-consumer-v1.json")
payload = json.loads(path.read_text(encoding="utf-8"))
assert payload["schema_version"] == "proof-consumer-eval.v1"
assert payload["automatic_publication"] is False
assert len(payload["sources"]) == 2
assert {source["source_type"] for source in payload["sources"]} == {
    "dedao_ebook",
    "wechat_mp_article",
}

citations = {
    citation_id
    for source in payload["sources"]
    for citation_id in source["citation_ids"]
}
cases = {case["case_id"]: case for case in payload["cases"]}
assert set(cases) == {
    "supported",
    "contradicted",
    "insufficient",
    "stale",
    "citation_missing",
}
assert cases["supported"]["classification"] == "supported"
assert cases["contradicted"]["classification"] == "contradicted"
for case_id in ("insufficient", "stale", "citation_missing"):
    assert cases[case_id]["classification"] == "unknown"

for case in cases.values():
    if case["classification"] != "unknown":
        assert case["citation_ids"]
        assert set(case["citation_ids"]) <= citations

assert cases["citation_missing"]["missing_citation_ids"]
assert not (set(cases["citation_missing"]["missing_citation_ids"]) <= citations)
assert cases["insufficient"]["feedback"]["outcome"] == "zero_hit"
assert cases["insufficient"]["feedback"]["creates"] == "gap"
for case_id in ("contradicted", "stale", "citation_missing"):
    assert cases[case_id]["feedback"]["creates"] == "reverification"
assert {case["feedback"]["outcome"] for case in cases.values()} == {
    "used",
    "rejected",
    "stale",
    "conflict",
    "zero_hit",
}

serialized = json.dumps(payload, sort_keys=True).lower()
for forbidden in ("source_body", "raw_prompt", "cookie", "authorization"):
    assert forbidden not in serialized
PY

go test ./backend/app -run 'KnowledgeFeedbackAssessmentRequiresReverificationForInvalidatingSignals|KnowledgeReverificationRunnerProducesCandidateWithoutPublishing|KnowledgeGaps' -count=1
