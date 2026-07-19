# Book Agent Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Publish versioned Agent Packages from authorized book and article releases, execute them through a policy-controlled runtime, and validate the contract with proof-oriented and health evidence consumers.

**Architecture:** KBase remains the source, compiler, release, and delivery control plane. A separate runtime consumes immutable Agent Packages, invokes models and MCP tools under deterministic policy, and reports bounded outcomes. Consumer products retain their own review, personal context, and final-action rules.

**Tech Stack:** Go, JSON Schema, private HTTP APIs, MCP, TokenPlan-compatible models, Python consumer adapters, Node frontend smoke tests, DeepEval/Ragas-compatible golden datasets, OpenTelemetry/Phoenix-compatible traces.

---

### Task 1: Define `agent-package.v1`

**Files:**
- Create: `contracts/agent-package-v1.schema.json`
- Create: `backend/app/agent_package.go`
- Create: `backend/app/agent_package_test.go`
- Modify: `docs/contracts/knowledge-supply-v1.md`

1. Write failing tests for package identity, pinned releases, retrieval policy,
   model policy, tool allowlist, safety policy, evaluation thresholds, and UI
   capabilities.
2. Run `go test ./backend/app -run AgentPackage -count=1` and confirm failure.
3. Implement schema validation and deterministic package hashing.
4. Reject unpublished releases, missing citations, unknown tools, and mutable
   release references.
5. Re-run the focused tests and commit only these files.

### Task 2: Store and publish Agent Packages

**Files:**
- Create: `backend/app/agent_package_store.go`
- Create: `backend/app/agent_package_store_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

1. Test atomic writes, idempotent publication, supersession, list/detail APIs,
   authorization, and stable URLs.
2. Add `GET /api/agent-packages`, `GET /api/agent-packages/{id}`, and explicit
   operator-only publication.
3. Return conflict for a reused idempotency key with different content.
4. Run focused HTTP and store tests.

### Task 3: Add package evaluation gates

**Files:**
- Create: `backend/app/agent_package_evaluation.go`
- Create: `backend/app/agent_package_evaluation_test.go`
- Create: `contracts/agent-evaluation-v1.schema.json`
- Create: `testdata/agent-evals/book-agent-v1.json`

1. Define golden cases for retrieval, citations, abstention, tool choice, and
   tool arguments without including copyrighted source bodies.
2. Test that required failed or missing metrics block publication.
3. Persist evaluation input hash, evaluator version, result, and timestamp.
4. Add a CI-safe deterministic evaluator adapter; keep LLM judging optional.

### Task 4: Expose read-only Agent resources and tools

**Files:**
- Modify: `backend/app/book_mcp.go`
- Modify: `backend/app/book_mcp_test.go`
- Create: `backend/app/agent_tool_policy.go`
- Create: `backend/app/agent_tool_policy_test.go`

1. Add package-scoped search, citation resolution, claim lookup, and package
   metadata resources.
2. Require an explicit package and release scope on every call.
3. Evaluate every proposed call as `allow`, `require_confirmation`, or `block`.
4. Keep the first release read-only and test argument rejection and audit data.

### Task 5: Build the proof consumer adapter

**Files in the proof consumer repository:**
- Create: `rpa_llm/kbase_release_consumer.py`
- Create: `tests/test_kbase_release_consumer.py`
- Modify: `rpa_llm/decision_engine.py`
- Modify: `rpa_llm/claim_verifier.py`

1. Test cursor-based package import and idempotent receipts.
2. Preserve release, claim, chunk, and citation identity in the local knowledge
   runtime.
3. Add package evidence to claim verification as support, contradiction, or
   unknown; never treat retrieval as proof by itself.
4. Send bounded used, rejected, stale, conflict, and zero-hit feedback.
5. Run the focused consumer, claim, and API contract tests.

### Task 6: Validate the proof pilot

**Files:**
- Create: `testdata/agent-evals/proof-consumer-v1.json`
- Create: `scripts/proof-consumer-contract-smoke.sh`
- Modify: `scripts/knowledge-contract-smoke.sh`

1. Build a fixture from one book release and a bounded related-article release.
2. Test supported, contradicted, insufficient, stale, and citation-missing
   claims.
3. Require resolvable citations and explicit unknown outcomes.
4. Verify feedback creates a gap or reverification candidate without automatic
   publication.

### Task 7: Extend the health consumer without bypassing review

**Files in the health consumer repository:**
- Modify: `backend/app/integrations/dedao_kbase_release_consumer.py`
- Modify: `backend/tests/test_dedao_kbase_release_consumer.py`
- Modify: `backend/app/tasks/system_knowledge_lifecycle.py`

1. Test import of evidence-only Agent Packages into draft artifacts.
2. Preserve package version, release lineage, safety flags, and evaluation
   status.
3. Hold stale, conflicting, unevaluated, or non-evidence-only packages.
4. Confirm that import never publishes to the serving index and never creates a
   diagnosis, prescription, dosage, or personal-data write.
5. Re-run integration, reconciliation, safety, and feedback-outbox tests.

### Task 8: Add runtime traces and replay

**Files:**
- Create: `contracts/agent-trace-v1.schema.json`
- Create: `backend/app/agent_trace.go`
- Create: `backend/app/agent_trace_test.go`

1. Record package and release versions, retrieval IDs and scores, model route,
   proposed tools, policy decisions, tool outcomes, and final citations.
2. Redact credentials, source bodies, private prompts, and consumer user data.
3. Add deterministic replay over stored evidence and mocked model/tool results.
4. Export OpenTelemetry-compatible spans for optional Phoenix ingestion.

### Task 9: Generate the shared Book App shell

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Add stable package, agent, and app routes.
2. Render only capabilities declared by `ui_manifest`.
3. Provide reader, search, grounded conversation, evidence inspection, and
   evaluation status without nested product forks.
4. Verify desktop and mobile layouts and that unavailable capabilities are
   explained rather than linked to empty pages.

### Task 10: Run release gates and production pilot

**Files:**
- Modify: `docs/dossiers/2026-07-19-book-agent-platform.md`
- Regenerate: `docs/_generated/system-map.json`

1. Run focused tests after every task, then `go test ./...` and frontend builds.
2. Run the package, proof consumer, and health consumer contract suites.
3. Run `go run ./cmd/system-map --root . --out docs/_generated/system-map.json`,
   `bash scripts/system-map-smoke.sh`, `bash scripts/privacy-smoke.sh`, and
   `git diff --check`.
4. Deploy from a clean main branch and verify package publication, proof import,
   health hold/import behavior, citations, receipts, and feedback closure.
5. Mark G3-G6 complete only when the exact deployed revision passes online
   verification.
