# Clinical Evidence Audit Agent Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Publish `book-agent-clinical-trials-truth@2.0.0` as a reproducible multi-source clinical-trial evidence audit product with structured reports and Proofroom projection.

**Architecture:** Extend the immutable Agent Package contract with a versioned evidence policy, then run audits through a durable coordinator backed by content-addressed JSON artifacts. The Web Agent workspace creates and follows audits through stable routes; Proofroom receives only validated structured projections.

**Tech Stack:** Go, existing KBase JSON stores and HTTP server, TokenPlan-compatible model client, vanilla JavaScript/CSS Web UI, Node smoke tests, Go contract and HTTP tests.

---

### Task 1: Add Agent Package v2 Evidence Policy

**Files:**
- Modify: `backend/app/agent_package.go`
- Modify: `backend/app/agent_package_test.go`
- Modify: `backend/app/agent_package_store_test.go`

**Step 1: Write failing contract tests**

Add tests proving that:

- v1 packages remain valid without an evidence policy;
- v2 requires `EvidencePolicy`;
- primary and supporting Release IDs must be pinned exactly once;
- primary Releases do not count as independent supporting evidence;
- allowed verdicts are limited to `supported`, `contradicted`, `mixed`, and
  `insufficient`;
- claim/evidence limits and report schema are validated;
- policy changes alter the package content hash.

**Step 2: Run the focused tests**

Run:

```bash
go test ./backend/app -run 'TestAgentPackageV2|TestAgentPackageHashBindsEvidencePolicy' -count=1
```

Expected: FAIL because Package v2 and `EvidencePolicy` do not exist.

**Step 3: Implement the contract**

Add:

- `AgentPackageSchemaVersionV1` and `AgentPackageSchemaVersionV2`;
- `AgentPackageEvidencePolicy`;
- source-role references and validation;
- canonical normalization for hashing;
- backward-compatible v1 loading and publication.

**Step 4: Run focused and package tests**

```bash
go test ./backend/app -run 'TestAgentPackage' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_package.go backend/app/agent_package_test.go backend/app/agent_package_store_test.go
git commit -m "feat(agent): add evidence policy package contract"
```

### Task 2: Define And Persist Immutable Evidence Audits

**Files:**
- Create: `backend/app/evidence_audit.go`
- Create: `backend/app/evidence_audit_test.go`
- Create: `backend/app/evidence_audit_store.go`
- Create: `backend/app/evidence_audit_store_test.go`

**Step 1: Write failing schema and store tests**

Cover:

- `evidence-audit.v1` strict validation;
- deterministic input and output hashes;
- valid verdict/citation combinations;
- confidence derived from source independence and citation completeness;
- immutable completed artifacts;
- idempotent creation for identical package/input hashes;
- conflict for reused idempotency keys with different inputs;
- list ordering and stable audit lookup.

**Step 2: Run the focused tests**

```bash
go test ./backend/app -run 'TestEvidenceAudit' -count=1
```

Expected: FAIL because the audit contract and store do not exist.

**Step 3: Implement the minimal contract and store**

Persist under `agent-audits/`:

- a manifest containing bounded task metadata;
- content-addressed input and report artifacts;
- explicit `queued`, `running`, `completed`, and `failed` states;
- package, Release, retrieval, model, and Trace identities.

Do not persist credentials, downloaded source bodies, or unrestricted prompts.

**Step 4: Run focused tests and race tests**

```bash
go test ./backend/app -run 'TestEvidenceAudit' -count=1
go test -race ./backend/app -run 'TestEvidenceAudit' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/evidence_audit*.go
git commit -m "feat(agent): persist immutable evidence audits"
```

### Task 3: Implement The Evidence Audit Runner

**Files:**
- Create: `backend/app/evidence_audit_runner.go`
- Create: `backend/app/evidence_audit_runner_test.go`
- Modify: `backend/app/agent_runtime.go`
- Modify: `backend/app/agent_trace.go`
- Modify: `backend/app/agent_trace_test.go`

**Step 1: Write failing workflow tests**

Use synthetic Releases and a fake model client to cover:

- primary claim selection;
- retrieval across every pinned supporting Release;
- source-aware evidence grouping and de-duplication;
- supported, contradicted, mixed, and insufficient decisions;
- forced downgrade when independent sources are below policy;
- unresolved citations, changed Release hashes, invalid JSON, timeout, and
  model failure;
- diagnosis/treatment questions producing a safe insufficient/abstained result;
- completed and failed Trace persistence.

**Step 2: Run the focused tests**

```bash
go test ./backend/app -run 'TestEvidenceAuditRunner|TestAgentTraceEvidenceAudit' -count=1
```

Expected: FAIL because the runner does not exist.

**Step 3: Implement deterministic stages**

Implement:

1. validate the published v2 Package and pinned Release hashes;
2. select up to the policy claim limit;
3. retrieve and resolve evidence per claim across supporting Releases;
4. call the package-selected TokenPlan model with strict JSON output;
5. validate verdicts and compute confidence in code;
6. persist the immutable report and Agent Trace.

Reuse existing package retrieval and citation resolvers instead of creating a
second search implementation.

**Step 4: Run workflow, runtime, and race tests**

```bash
go test ./backend/app -run 'TestEvidenceAudit|TestAgentPackageRuntime|TestAgentTrace' -count=1
go test -race ./backend/app -run 'TestEvidenceAuditRunner' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/evidence_audit_runner.go backend/app/evidence_audit_runner_test.go backend/app/agent_runtime.go backend/app/agent_trace.go backend/app/agent_trace_test.go
git commit -m "feat(agent): run multi-source evidence audits"
```

### Task 4: Add A Durable Audit Coordinator And HTTP API

**Files:**
- Create: `backend/app/evidence_audit_coordinator.go`
- Create: `backend/app/evidence_audit_coordinator_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`

**Step 1: Write failing API and recovery tests**

Cover:

- create returns `202` and an audit ID;
- identical input returns the existing audit;
- list and detail routes;
- invalid version, non-v2 package, oversized claim selection, and unknown
  audit errors;
- bounded worker concurrency;
- queued/running work is safely recovered after coordinator restart;
- failed audits remain inspectable and retry requires a new explicit request.

**Step 2: Run focused tests**

```bash
go test ./backend/app ./cmd/kbase-server -run 'TestEvidenceAuditHTTP|TestEvidenceAuditCoordinator' -count=1
```

Expected: FAIL because routes and coordinator do not exist.

**Step 3: Implement coordinator and routes**

Add:

- `POST /api/agent-packages/{id}/audits?version=...`;
- `GET /api/agent-packages/{id}/audits?version=...`;
- `GET /api/agent-audits/{audit_id}`;
- a bounded coordinator started and closed by `cmd/kbase-server`;
- restart recovery for queued/running tasks;
- body limits and existing bearer authentication.

**Step 4: Run focused tests and server tests**

```bash
go test ./backend/app ./cmd/kbase-server -run 'TestEvidenceAudit|TestKBaseHTTPHandler' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/evidence_audit_coordinator.go backend/app/evidence_audit_coordinator_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go
git commit -m "feat(agent): expose asynchronous evidence audits"
```

### Task 5: Add Proofroom Projection And Explicit Delivery

**Files:**
- Create: `backend/app/evidence_audit_proofroom.go`
- Create: `backend/app/evidence_audit_proofroom_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`

**Step 1: Write failing projection and delivery tests**

Verify:

- only completed, valid audits can project;
- projection preserves claim, verdict, citation, limitation, and action
  identity without source bodies;
- preview is read-only;
- delivery is explicit and idempotent;
- missing Proofroom configuration returns `503`;
- remote rejection is visible and does not mark the audit delivered.

**Step 2: Run focused tests**

```bash
go test ./backend/app ./cmd/kbase-server -run 'TestProofroom|TestKBaseHTTPHandlerProofroom' -count=1
```

Expected: FAIL because projection does not exist.

**Step 3: Implement projection and client**

Add:

- `GET /api/agent-audits/{audit_id}/proofroom`;
- `POST /api/agent-audits/{audit_id}/proofroom`;
- bounded Proofroom payload and delivery receipt;
- environment-only endpoint/token configuration;
- no automatic delivery from audit completion.

**Step 4: Run focused tests**

```bash
go test ./backend/app ./cmd/kbase-server -run 'TestProofroom|TestKBaseHTTPHandlerProofroom' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/evidence_audit_proofroom.go backend/app/evidence_audit_proofroom_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go
git commit -m "feat(agent): project audits to Proofroom"
```

### Task 6: Build The Evidence Audit Web Workspace

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Create: `frontend-web/scripts/evidence-audit-agent-smoke.mjs`

**Step 1: Write failing Web smoke assertions**

Require:

- stable `/agents/{id}/audits/{audit_id}?version=...` route parsing;
- package scope and independent-source status;
- primary audit composer and claim selection;
- queued/running/failed/completed states;
- verdict summary, evidence matrix, limitations, gaps, and review actions;
- citation links and Proofroom preview/delivery;
- grounded chat rendered after the audit workspace;
- no false audit UI for v1 packages.

**Step 2: Run Web smokes**

```bash
node frontend-web/scripts/evidence-audit-agent-smoke.mjs
```

Expected: FAIL because the audit workspace is absent.

**Step 3: Implement the workspace**

Use the existing shared shell and Agent route switch. Keep the main report
wide, use expandable evidence rows rather than a horizontally scrolling table,
render model text through the existing safe Markdown renderer, and poll only
while the current audit is queued/running.

**Step 4: Run syntax, smoke, and visual checks**

```bash
node --check frontend-web/app.js
for test in frontend-web/scripts/*smoke*.mjs; do node "$test"; done
```

Then use Playwright at desktop and 390 px mobile widths to verify creation,
progress, report, stable URL, Proofroom preview, and no overflow.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html frontend-web/scripts/book-knowledge-web-smoke.mjs frontend-web/scripts/evidence-audit-agent-smoke.mjs
git commit -m "feat(web): add clinical evidence audit workspace"
```

### Task 7: Extend Evaluation Gates For Audit Behavior

**Files:**
- Modify: `backend/app/agent_package_evaluation.go`
- Modify: `backend/app/agent_package_evaluation_test.go`
- Create: `backend/app/evidence_audit_evaluation_test.go`
- Create: `contracts/agent-package-v2.schema.json`
- Create: `contracts/evidence-audit-v1.schema.json`

**Step 1: Write failing evaluation tests**

Add metrics for:

- adjudication consistency;
- source independence;
- conflict detection;
- report citation completeness;
- audit abstention/safe insufficiency;
- Proofroom projection completeness.

Require non-zero thresholds for v2 publication.

**Step 2: Run focused evaluation tests**

```bash
go test ./backend/app -run 'TestAgentPackageEvaluation|TestEvidenceAuditEvaluation' -count=1
```

Expected: FAIL until metrics and gates exist.

**Step 3: Implement evaluation adapters and schemas**

Keep fixtures synthetic and deterministic. Do not add downloaded book or
article text.

**Step 4: Run focused and schema checks**

```bash
go test ./backend/app -run 'TestAgentPackageEvaluation|TestEvidenceAuditEvaluation' -count=1
python3 -m json.tool contracts/agent-package-v2.schema.json >/dev/null
python3 -m json.tool contracts/evidence-audit-v1.schema.json >/dev/null
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_package_evaluation.go backend/app/agent_package_evaluation_test.go backend/app/evidence_audit_evaluation_test.go contracts/agent-package-v2.schema.json contracts/evidence-audit-v1.schema.json
git commit -m "feat(agent): gate clinical evidence audit quality"
```

### Task 8: Update Architecture Inventory And Delivery Dossier

**Files:**
- Create: `docs/dossiers/2026-07-23-clinical-evidence-audit-agent.md`
- Modify: generated system-map files through the repository generator
- Modify: `README.md` only if the public API/architecture overview requires it

**Step 1: Generate system-map artifacts**

Run the repository system-map generator identified by
`scripts/system-map-smoke.sh`; do not hand-edit architecture counts.

**Step 2: Record Gates G1-G4**

Document requirements, approved design, threat boundaries, exact tests,
independent review findings, and unresolved production prerequisites.

**Step 3: Run full verification**

```bash
go mod verify
go test ./... -count=1
go test -race ./backend/app ./cmd/kbase-server -count=1
go vet ./...
npm --prefix frontend run build
for test in frontend-web/scripts/*smoke*.mjs; do node "$test"; done
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS. Existing build warnings must be recorded, not hidden.

**Step 4: Request code review**

Review contract compatibility, durable recovery, citation fail-closed behavior,
medical safety, Proofroom data minimization, and responsive UI.

**Step 5: Commit**

```bash
git add docs/dossiers/2026-07-23-clinical-evidence-audit-agent.md docs/_generated docs/system-map README.md
git commit -m "docs(agent): record clinical audit delivery gates"
```

### Task 9: Assemble, Evaluate, Publish, Deploy, And Verify v2.0.0

**Files:**
- No private Package input or real evaluation corpus is committed.
- Update: `docs/dossiers/2026-07-23-clinical-evidence-audit-agent.md`

**Step 1: Assemble the private production Package**

Pin:

- the current primary clinical-trials book Release;
- reviewed related Dedao/WeChat supporting Releases;
- explicit source roles and citation allowlists;
- the actually configured TokenPlan model;
- read-only MCP tools;
- the `evidence-audit.v1` report schema and v2 thresholds.

**Step 2: Run private deterministic and real-model evaluation**

Publication remains blocked unless every required metric passes and all
citations resolve.

**Step 3: Publish `book-agent-clinical-trials-truth@2.0.0`**

Use publisher-only evaluate/publish endpoints. Confirm `1.0.0` and `1.1.0`
remain immutable and `2.0.0` becomes the published version.

**Step 4: Deploy exact clean-main source**

Build from the pushed commit, verify archive hashes server-side, retain binary
and frontend backups, and use automatic rollback on restart or health failure.

**Step 5: Execute production pilot**

Create one real audit, wait for completion, resolve every citation, inspect the
Trace, preview the Proofroom projection, and verify no source bodies or secrets
are present. Do not deliver to Proofroom until the preview is explicitly
accepted.

**Step 6: Record G5-G6 and commit**

Record release commit, archive/binary hashes, backup paths, service health,
public route verification, audit ID, Trace ID, and Proofroom preview outcome.

```bash
git add docs/dossiers/2026-07-23-clinical-evidence-audit-agent.md
git commit -m "docs(agent): record clinical audit production pilot"
git push origin main
```
