# Clinical Trial Truth Agent Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn `book-agent-clinical-trials-truth` into a durable, evidence-only product that audits an NCT identifier, DOI/PMID, or bounded clinical-trial claim against pinned primary evidence and produces a reviewable report.

**Architecture:** Reuse the existing Agent Package, Knowledge Release, policy, trace, source-run, and consumer contracts. Add immutable clinical source snapshots, deterministic trial comparisons, a leased audit worker, and a task-oriented Web surface. The model explains deterministic findings but cannot replace them, broaden tool scope, or publish into a downstream serving index.

**Tech Stack:** Go, SQLite, JSON Schema, ClinicalTrials.gov API v2, NCBI E-utilities, TokenPlan-compatible chat, MCP-compatible read-only tools, vanilla JavaScript/CSS, Node smoke tests, and Python downstream contract tests.

---

### Task 1: Define The Clinical Audit Contract

**Files:**
- Create: `contracts/clinical-trial-audit-v1.schema.json`
- Create: `backend/app/clinical_trial_audit.go`
- Create: `backend/app/clinical_trial_audit_test.go`
- Modify: `docs/contracts/knowledge-supply-v1.md`

1. Write failing tests for valid NCT, DOI, PMID, and claim inputs; stable input
   hashes; source fingerprints; finding classes; citations; confidence;
   limitations; and allowed run states.
2. Add a test that removes a finding's citations and expects
   `ValidateClinicalTrialAudit` to fail.
3. Run `go test ./backend/app -run ClinicalTrialAudit -count=1`; expect FAIL
   because the contract is missing.
4. Define `ClinicalTrialAudit`, `ClinicalTrialAuditRequest`,
   `ClinicalTrialSourceSnapshot`, `ClinicalTrialFinding`, and
   `ClinicalTrialAuditRun`.
5. Normalize identifiers before hashing. Reject unknown finding classes,
   duplicate citation IDs, uncited factual findings, unknown run states, and
   incomplete terminal audits.
6. Add the JSON Schema and run:

   ```bash
   jq empty contracts/clinical-trial-audit-v1.schema.json
   go test ./backend/app -run ClinicalTrialAudit -count=1
   ```

   Expected: PASS.
7. Commit only these files with
   `feat(agent): define clinical trial audit contract`.

### Task 2: Add A Durable Audit Store

**Files:**
- Create: `backend/app/clinical_trial_audit_store.go`
- Create: `backend/app/clinical_trial_audit_store_test.go`

1. Write failing tests for create, idempotent replay, idempotency conflict, get,
   list, single-run lease, checkpoint transition, retry, terminal failure, and
   expired-lease recovery.
2. Run `go test ./backend/app -run ClinicalTrialAuditStore -count=1`; expect
   FAIL.
3. Add SQLite tables for runs, snapshots, findings, results, and idempotency
   keys. Use transactions for create, lease, and checkpoint writes.
4. Persist bounded structured data only; never persist credentials or private
   prompt text in the run record.
5. Run:

   ```bash
   go test ./backend/app -run ClinicalTrialAuditStore -count=1
   go test -race ./backend/app -run ClinicalTrialAuditStore -count=1
   ```

   Expected: PASS.
6. Commit with `feat(agent): persist durable clinical audits`.

### Task 3: Add The ClinicalTrials.gov Client

**Files:**
- Create: `backend/app/clinical_trials_gov.go`
- Create: `backend/app/clinical_trials_gov_test.go`

1. Build synthetic `httptest.Server` fixtures for a study, API version, record
   history, not-found, rate limit, timeout, malformed JSON, and schema drift.
2. Test that `GetStudy("NCT01234567")` returns source type
   `clinicaltrials_gov_study`, canonical identity, upstream data timestamp,
   retrieval timestamp, normalized fields, and a content hash.
3. Run `go test ./backend/app -run ClinicalTrialsGov -count=1`; expect FAIL.
4. Implement bounded read-only calls to `/api/v2/studies/{nctId}` and
   `/api/v2/version`. Normalize only fields needed for identity, protocol,
   status, results, references, and deterministic comparison.
5. Add typed not-found, rate-limit, timeout, and schema errors. Mark only
   transient failures retryable.
6. Re-run the focused tests; expect PASS without external network access.
7. Commit with `feat(agent): collect clinical trial registry evidence`.

### Task 4: Add PubMed And Identifier Resolution

**Files:**
- Create: `backend/app/pubmed.go`
- Create: `backend/app/pubmed_test.go`
- Create: `backend/app/clinical_trial_identifier.go`
- Create: `backend/app/clinical_trial_identifier_test.go`

1. Write failing tests for DOI-to-PMID, PMID metadata, NCT references, multiple
   candidate publications, no match, retryable NCBI failures, and bounded
   result counts.
2. Run
   `go test ./backend/app -run 'PubMed|ClinicalTrialIdentifier' -count=1`;
   expect FAIL.
3. Implement bounded ESearch, ESummary, and EFetch adapters. Read optional API
   configuration from environment; do not fetch publisher full text.
4. Normalize DOI, PMID, NCT, and claim input. Preserve multiple candidates and
   return `awaiting_review` instead of selecting silently when identity is
   ambiguous.
5. Re-run focused tests; expect PASS.
6. Commit with `feat(agent): resolve trial publications`.

### Task 5: Build Deterministic Trial Comparison

**Files:**
- Create: `backend/app/clinical_trial_compare.go`
- Create: `backend/app/clinical_trial_compare_test.go`
- Create: `testdata/clinical-trial-audits/golden-v1.json`

1. Write table-driven tests for enrollment, arms, allocation, masking,
   primary/secondary endpoints, outcome time frames, analysis population,
   completion/result/publication dates, missing fields, and changed registry
   versions.
2. Add a primary-endpoint-switch case that must emit a high-risk
   `deterministic_discrepancy` with registry and publication citations.
3. Run `go test ./backend/app -run CompareClinicalTrial -count=1`; expect FAIL.
4. Implement a pure, side-effect-free comparator. Findings carry field paths,
   before/after values, severity, and citations. The model does not decide
   whether normalized fields differ.
5. Add synthetic supported, contradicted, unknown, stale, ambiguous, and unsafe
   golden cases without copyrighted source bodies or patient data.
6. Re-run focused tests; expect PASS.
7. Commit with `feat(agent): compare registered and reported trials`.

### Task 6: Add Grounded Audit Synthesis

**Files:**
- Create: `backend/app/clinical_trial_synthesis.go`
- Create: `backend/app/clinical_trial_synthesis_test.go`
- Modify: `backend/app/agent_trace_test.go`

1. Write failing tests for structured output, citation resolution,
   unsupported-finding rejection, abstention, cost/timeout enforcement, and
   trace redaction.
2. Add a fake model response with an invented finding ID and require
   `ErrClinicalAuditUngroundedOutput`.
3. Run
   `go test ./backend/app -run 'ClinicalTrialSynthesis|AgentTrace' -count=1`;
   expect FAIL.
4. Send only normalized facts, deterministic findings, and citation metadata to
   the package model. Require `clinical-trial-audit.v1` output.
5. Reject unknown finding/citation IDs and return explicit abstention when
   grounding validation fails.
6. Re-run focused tests; expect PASS.
7. Commit with `feat(agent): synthesize grounded trial audits`.

### Task 7: Add The Leased Audit Runner

**Files:**
- Create: `backend/app/clinical_trial_audit_runner.go`
- Create: `backend/app/clinical_trial_audit_runner_test.go`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

1. Write failing tests for stage checkpoints, restart recovery, duplicate
   workers, retry/backoff, partial evidence, abstention, terminal completion,
   and graceful shutdown.
2. Run
   `go test ./backend/app ./cmd/kbase-server -run ClinicalTrialAuditRunner -count=1`;
   expect FAIL.
3. Implement one-stage-at-a-time execution with a durable checkpoint after
   resolution, collection, comparison, synthesis, and review preparation.
4. Requeue transient upstream/model failures. Abstain on invalid identity or
   unavailable required primary evidence. Never let the browser request own
   the model call.
5. Wire a background runner into `cmd/kbase-server/main.go` with bounded,
   portable environment configuration for interval, lease, and timeouts.
6. Run focused normal and race tests; expect PASS.
7. Commit with `feat(agent): run durable clinical trial audits`.

### Task 8: Expose Stable Audit APIs

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `contracts/agent-package-v1.schema.json`
- Modify: `backend/app/agent_package.go`
- Modify: `backend/app/agent_package_test.go`

1. Write failing HTTP tests for:
   - `POST /api/agent-packages/{id}/audits?version={version}`;
   - `GET /api/agent-audits/{run_id}`;
   - `GET /api/agent-audits?package_id={id}&after={cursor}&limit={n}`;
   - `POST /api/agent-audits/{run_id}/retry`.
2. Cover authentication, package/version pinning, body limits, idempotency,
   pagination, terminal state, conflict, and not found.
3. Run `go test ./backend/app -run 'AgentAuditHTTP|AgentPackage' -count=1`;
   expect FAIL.
4. Implement handlers returning `202 Accepted` and a stable `Location` for new
   runs. Add `clinical_trial_audit` to `ui_manifest`.
5. Permit that capability only for a domain-evaluated, citation-required,
   `evidence_only` package with the audit tool allowlist.
6. Re-run focused tests; expect PASS.
7. Commit with `feat(agent): expose clinical audit APIs`.

### Task 9: Add Read-Only Clinical Tools

**Files:**
- Modify: `backend/app/book_mcp.go`
- Modify: `backend/app/book_mcp_test.go`
- Modify: `backend/app/agent_tool_policy.go`
- Modify: `backend/app/agent_tool_policy_test.go`

1. Write failing tests for exact schemas, package/version scope, NCT/PMID
   normalization, bounded results, blocked arbitrary URLs, blocked write
   methods, and blocked unknown arguments.
2. Run
   `go test ./backend/app -run 'ClinicalTrialTool|AgentToolPolicy|BookKnowledgeMCP' -count=1`;
   expect FAIL.
3. Add typed read-only tools for registry study/history, PubMed search/record,
   deterministic comparison, and evidence resolution.
4. Return snapshot IDs and normalized fields rather than unrestricted upstream
   bodies. Keep all calls inside the package policy and trace boundary.
5. Re-run focused tests; expect PASS.
6. Commit with `feat(agent): add clinical evidence tools`.

### Task 10: Build The Task-Oriented Web Product

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Extend the smoke test to require Audit NCT, Compare publication, and Verify
   claim; durable submission; stable run URL; polling; Markdown; structured
   evidence; citations; and failed/abstained states.
2. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs`; expect FAIL.
3. Add `/agents/{package_id}/audits/{run_id}` while retaining package version in
   the query string. Move generic chat below the completed audit.
4. Render conclusion, confidence, timeline, field comparisons, discrepancies,
   evidence classes, citations, limitations, review status, and retry.
5. Use a wide main report with a collapsible evidence panel. Do not show raw
   JSON by default and do not nest cards.
6. Run:

   ```bash
   node --check frontend-web/app.js
   node frontend-web/scripts/book-knowledge-web-smoke.mjs
   ```

   Expected: PASS.
7. Verify desktop and mobile screenshots with Playwright: no blank state,
   overlap, clipped citation, or polling layout shift.
8. Commit with `feat(agent): add clinical trial audit workspace`.

### Task 11: Add The Domain Evaluation Gate And Package `1.2.0`

**Files:**
- Create: `backend/app/clinical_trial_evaluation.go`
- Create: `backend/app/clinical_trial_evaluation_test.go`
- Create: `contracts/clinical-trial-evaluation-v1.schema.json`
- Create: `scripts/clinical-trial-agent-smoke.sh`
- Modify: `backend/app/agent_package_evaluation.go`
- Modify: `backend/app/agent_package_evaluation_test.go`

1. Write failing tests for identifier accuracy, extraction accuracy,
   discrepancy detection, citation resolution, evidence attribution, unsafe
   abstention, stale data, prompt injection, latency, cost, idempotency, and
   redaction.
2. Require `unsafe_abstention == 1.0` and zero unresolved citations.
3. Run `go test ./backend/app -run ClinicalTrialEvaluation -count=1`; expect
   FAIL.
4. Implement deterministic domain evaluation over `golden-v1.json`, persisting
   evaluator version, input hash, case outcomes, metrics, and failure reasons.
5. Keep the runtime suite as a prerequisite. Do not let runtime input lower
   package thresholds.
6. Build a private `1.2.0` payload outside Git: hybrid retrieval,
   `evidence_only`, mandatory citations, `human_domain_review`, six read-only
   tools, and `clinical_trial_audit` UI capability.
7. Run focused evaluation tests and
   `bash scripts/clinical-trial-agent-smoke.sh`; expect PASS.
8. Commit code and synthetic fixtures only with
   `feat(agent): gate clinical trial audit quality`.

### Task 12: Validate Consumer Boundaries And Release

**Files in the proof consumer repository:**
- Modify: `rpa_llm/kbase_release_consumer.py`
- Modify: `tests/test_kbase_release_consumer.py`

**Files in the health consumer repository:**
- Modify: `backend/app/integrations/dedao_kbase_release_consumer.py`
- Modify: `backend/tests/test_dedao_kbase_release_consumer.py`
- Modify: `backend/app/tasks/system_knowledge_lifecycle.py`

**KBase files:**
- Modify: `docs/dossiers/2026-07-22-clinical-trial-truth-agent.md`
- Regenerate when required: `docs/_generated/system-map.json`

1. In the proof consumer, write a failing import test for audit finding,
   snapshot, and citation identity. Implement support, contradiction, unknown,
   and unresolved-conflict projection plus bounded feedback.
2. In the health consumer, write a failing test requiring proof review and
   `evidence_only`. Hold missing, stale, conflicting, or unevaluated audits;
   assert import creates only a review draft and leaves serving unchanged.
3. Commit independently in each repository after its focused/full tests,
   privacy checks, and review. Do not combine repository histories.
4. Run KBase G3 without piping output through plain `tail`:

   ```bash
   go test ./backend/app ./cmd/kbase-server -run 'ClinicalTrial|AgentPackage|AgentTool' -count=1
   go test -race ./backend/app ./cmd/kbase-server -run 'ClinicalTrial|AgentPackage|AgentTool' -count=1
   go test ./...
   node --check frontend-web/app.js
   node frontend-web/scripts/book-knowledge-web-smoke.mjs
   bash scripts/clinical-trial-agent-smoke.sh
   ```

5. If routes, tools, source operations, or durable objects changed, regenerate
   the system map. Then run:

   ```bash
   bash scripts/system-map-smoke.sh
   bash scripts/privacy-smoke.sh
   git diff --check
   ```

6. Complete independent G4 review of medical boundaries, SSRF resistance,
   rate limits, trace redaction, policy, citation integrity, and downstream
   isolation. Any Critical, High, or Medium finding returns to implementation.
7. Deploy from clean main. Record exact revision, hashes, rollback point,
   service status, restart count, public health, and authenticated smoke.
8. Run one synthetic-safe NCT audit and one missing-evidence case in production.
   Verify stable URLs, restart-safe completion, citations, abstention, proof
   verdict, health draft hold, traces, receipts, and bounded feedback.
9. Close the dossier as `shipped` only after user confirmation of the real
   production workflow.

