# Agent Evolution Control Plane Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn `/agent-packages` into a dense, human-controlled evolution inbox and connect Agent-policy and knowledge-release feedback loops through one durable control plane.

**Architecture:** Add a SQLite-backed evolution state machine beside the existing knowledge store, expose a small authenticated control-plane API, and let independent generation, evaluation, and release workers lease bounded jobs. Reuse existing Agent Package compilation, trusted evaluation, publication, knowledge feedback, reverification, runtime traces, and immutable release artifacts instead of duplicating their rules.

**Tech Stack:** Go, SQLite, existing KBase HTTP server, independent Go workers, vanilla JavaScript, semantic HTML/CSS, Node smoke tests, systemd deployment.

---

## Delivery gates

Execute in four acceptance layers. Stop and review after each layer:

1. Tasks 1–6: read-only evolution inbox and Agent fleet view.
2. Tasks 7–8: automatic candidate generation and deterministic evaluation; no publication.
3. Tasks 9–10: human approval, restricted publication, observation, and rollback.
4. Tasks 11–13: closed-loop signals, production delivery, and final verification.

Do not enable a later layer while an earlier layer has failing tests, unresolved review findings, or a blocked dossier gate.

### Task 1: Create the feature dossier and domain state contract

**Files:**
- Create: `docs/dossiers/2026-08-11-agent-evolution-control-plane.md`
- Create: `backend/app/evolution_control.go`
- Test: `backend/app/evolution_control_test.go`

**Step 1: Create the dossier skeleton**

Record the approved design, the four delivery layers, G1 admission decision, current branch, and links to both plan documents. Leave later gates explicitly pending; do not mark them passed in advance.

**Step 2: Write failing state-transition tests**

Cover the main path and every terminal/side state:

```go
func TestEvolutionRunTransitionContract(t *testing.T) {
    allowed := [][2]EvolutionRunStatus{
        {EvolutionDetected, EvolutionTriaged},
        {EvolutionTriaged, EvolutionGenerating},
        {EvolutionGenerating, EvolutionEvaluating},
        {EvolutionEvaluating, EvolutionAwaitingApproval},
        {EvolutionAwaitingApproval, EvolutionApproved},
        {EvolutionApproved, EvolutionPublishing},
        {EvolutionPublishing, EvolutionObserving},
        {EvolutionObserving, EvolutionCompleted},
    }
    for _, transition := range allowed {
        if err := ValidateEvolutionTransition(transition[0], transition[1]); err != nil {
            t.Fatalf("transition %s -> %s: %v", transition[0], transition[1], err)
        }
    }
    if err := ValidateEvolutionTransition(EvolutionDetected, EvolutionPublishing); err == nil {
        t.Fatal("detected run skipped approval")
    }
}
```

Also test that `rejected`, `completed`, `superseded`, `rolled_back`, and exhausted `blocked` runs cannot resume without an explicit retry that creates a new attempt.

**Step 3: Run the test to verify RED**

Run: `go test ./backend/app -run 'TestEvolutionRunTransitionContract'`

Expected: FAIL because the evolution types and validator do not exist.

**Step 4: Implement the minimal domain contract**

Define typed constants and bounded public records:

```go
type EvolutionRunStatus string
type EvolutionRunType string

const (
    EvolutionDetected         EvolutionRunStatus = "detected"
    EvolutionTriaged          EvolutionRunStatus = "triaged"
    EvolutionGenerating       EvolutionRunStatus = "generating"
    EvolutionEvaluating       EvolutionRunStatus = "evaluating"
    EvolutionAwaitingApproval EvolutionRunStatus = "awaiting_approval"
    EvolutionApproved         EvolutionRunStatus = "approved"
    EvolutionPublishing       EvolutionRunStatus = "publishing"
    EvolutionObserving        EvolutionRunStatus = "observing"
    EvolutionCompleted        EvolutionRunStatus = "completed"
    EvolutionBlocked          EvolutionRunStatus = "blocked"
    EvolutionRejected         EvolutionRunStatus = "rejected"
    EvolutionFailed           EvolutionRunStatus = "failed"
    EvolutionSuperseded       EvolutionRunStatus = "superseded"
    EvolutionRolledBack       EvolutionRunStatus = "rolled_back"
)
```

Add `EvolutionSignal`, `EvolutionRun`, `EvolutionCandidate`, `EvolutionScorecard`, `EvolutionApproval`, `EvolutionObservation`, and `EvolutionEvent` with JSON fields from the design. Keep free-text fields bounded and keep artifact bodies out of control-plane records.

**Step 5: Run focused tests**

Run: `go test ./backend/app -run 'TestEvolution'`

Expected: PASS.

**Step 6: Commit**

Stage only the dossier and Task 1 Go files, then commit:

```text
feat(kbase): define evolution control states
```

### Task 2: Build the SQLite evolution store and transactional event log

**Files:**
- Create: `backend/app/evolution_store.go`
- Test: `backend/app/evolution_store_test.go`
- Modify: `backend/app/evolution_control.go`

**Step 1: Write failing schema and persistence tests**

Test that a temporary store creates the approved tables, persists a run across reopen, and writes a run update plus event in one transaction. Include a failure hook proving neither record commits when event insertion fails.

```go
func TestEvolutionStoreTransitionsRunAndEventAtomically(t *testing.T) {
    store := newEvolutionTestStore(t)
    run := saveDetectedEvolutionRun(t, store)
    updated, err := store.TransitionRun(run.RunID, EvolutionTriaged, EvolutionTransitionInput{
        Actor: "operator",
        Code:  "triaged",
    })
    if err != nil || updated.Status != EvolutionTriaged {
        t.Fatalf("transition = %#v, %v", updated, err)
    }
    events := mustListEvolutionEvents(t, store, run.RunID)
    if len(events) != 2 || events[1].ToStatus != EvolutionTriaged {
        t.Fatalf("events = %#v", events)
    }
}
```

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestEvolutionStore'`

Expected: FAIL because the store does not exist.

**Step 3: Implement schema version 1**

Create `evolution_control.sqlite3` under the configured KBase root with:

```sql
CREATE TABLE evolution_signals (...);
CREATE TABLE evolution_runs (...);
CREATE TABLE evolution_candidates (...);
CREATE TABLE evolution_scorecards (...);
CREATE TABLE evolution_approvals (...);
CREATE TABLE evolution_observations (...);
CREATE TABLE evolution_events (...);
CREATE TABLE evolution_worker_leases (...);
CREATE TABLE evolution_outbox (...);
CREATE TABLE evolution_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

Use foreign keys, UTC RFC3339Nano timestamps, unique idempotency keys, and indexes for `status`, `package_id`, `risk_level`, `updated_at`, and pending outbox delivery. Follow the SQLite transaction and busy-timeout patterns in `backend/app/source_sync.go`; do not introduce a new database library.

**Step 4: Implement store primitives**

Add:

```go
func OpenEvolutionControlStore(root string, now func() time.Time) (*EvolutionControlStore, error)
func (s *EvolutionControlStore) CreateRun(input EvolutionRunInput) (*EvolutionRun, bool, error)
func (s *EvolutionControlStore) LoadRun(runID string) (*EvolutionRun, error)
func (s *EvolutionControlStore) TransitionRun(runID string, to EvolutionRunStatus, input EvolutionTransitionInput) (*EvolutionRun, error)
func (s *EvolutionControlStore) ListEvents(runID, after string, limit int) ([]EvolutionEvent, error)
```

Every mutation writes an event in the same transaction. Reject unknown status values and overlong public messages before opening the transaction.

**Step 5: Test migration, reopen, and concurrency**

Run:

```bash
go test ./backend/app -run 'TestEvolutionStore'
go test -race ./backend/app -run 'TestEvolutionStore'
```

Expected: PASS with no race report.

**Step 6: Commit**

Commit exact Task 2 files:

```text
feat(kbase): persist evolution control state
```

### Task 3: Ingest, deduplicate, aggregate, and prioritize signals

**Files:**
- Create: `backend/app/evolution_signals.go`
- Test: `backend/app/evolution_signals_test.go`
- Modify: `backend/app/evolution_store.go`

**Step 1: Write failing signal tests**

Cover:

- replay of the same idempotency key returns the original signal;
- payload changes under the same key return conflict;
- repeated signals inside the cooldown update aggregation instead of creating duplicate runs;
- related Agent and knowledge signals produce one `combined` run;
- risk, impact, expected benefit, and wait time produce stable ordering;
- user text, tokens, filesystem paths, and unrestricted model output are rejected or removed.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestEvolutionSignal'`

Expected: FAIL because ingestion is not implemented.

**Step 3: Implement bounded ingestion**

Add:

```go
type EvolutionSignalInput struct {
    IdempotencyKey string
    SignalType     string
    SourceType     string
    SourceID       string
    PackageID      string
    ReleaseID      string
    Severity       string
    ObservedValue  float64
    BaselineValue  float64
    EvidenceRefs   []string
    ObservedAt     time.Time
}

func (s *EvolutionControlStore) IngestSignal(input EvolutionSignalInput) (*EvolutionSignal, *EvolutionRun, bool, error)
```

Use a deterministic deduplication key based on signal type and affected immutable identities. Store only allowlisted signal types and reason codes.

**Step 4: Implement read models**

Add `EvolutionOverview`, `EvolutionRunPage`, and Agent fleet projections. Group `AgentPackageRecord` values by `package_id`, select the current `published` version, retain superseded versions as history, and join open runs by Package identity.

**Step 5: Run focused tests**

Run: `go test ./backend/app -run 'TestEvolutionSignal|TestEvolutionOverview'`

Expected: PASS.

**Step 6: Commit**

```text
feat(kbase): aggregate evolution signals
```

### Task 4: Add durable worker leases, retries, and outbox delivery

**Files:**
- Create: `backend/app/evolution_worker.go`
- Test: `backend/app/evolution_worker_test.go`
- Modify: `backend/app/evolution_store.go`

**Step 1: Write failing lease tests**

Test concurrent claims, capability filtering, lease renewal, lease loss, stale lease recovery, bounded retry attempts, and idempotent result submission. Prove two workers cannot own the same work item and a repeated publication result cannot execute twice.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestEvolutionWorker'`

Expected: FAIL.

**Step 3: Implement the work protocol**

Use explicit capabilities:

```go
const (
    EvolutionCapabilityKnowledge  = "knowledge_evolution"
    EvolutionCapabilityAgent      = "agent_evolution"
    EvolutionCapabilityEvaluation = "evaluation"
    EvolutionCapabilityRelease    = "release"
    EvolutionCapabilityObserve    = "observation"
)
```

Implement `LeaseNextEvolutionWork`, `RenewEvolutionLease`, `CompleteEvolutionWork`, `FailEvolutionWork`, and `RecoverExpiredEvolutionLeases`. Each result includes the worker ID, attempt, lease identity, idempotency key, and bounded artifact reference.

**Step 4: Implement outbox consistency**

Write state changes and outbox messages in one transaction. Delivery receipts are idempotent. A dead-lettered outbox item moves its run to `blocked` with a finite error code; it never reports success.

**Step 5: Run concurrency tests**

Run:

```bash
go test ./backend/app -run 'TestEvolutionWorker'
go test -race ./backend/app -run 'TestEvolutionWorker'
```

Expected: PASS.

**Step 6: Commit**

```text
feat(kbase): lease evolution worker tasks
```

### Task 5: Expose read-only evolution HTTP APIs

**Files:**
- Create: `backend/app/kbase_http_evolution.go`
- Test: `backend/app/kbase_http_evolution_test.go`
- Modify: `backend/app/kbase_http.go:104-270`
- Modify: `backend/app/kbase_http.go:320-520`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Write failing API tests**

Require authenticated responses for:

```text
GET /api/evolution/overview
GET /api/evolution/runs
GET /api/evolution/runs/:run_id
GET /api/evolution/runs/:run_id/events
```

Test status/risk/type/package filters, cursor pagination, invalid IDs, missing runs, bounded limits, stable empty arrays, and response privacy. Test `KBASE_EVOLUTION_ENABLED=0` returns a clear unavailable response without affecting existing Package APIs.

**Step 2: Verify RED**

Run: `go test ./backend/app ./cmd/kbase-server -run 'Test.*Evolution'`

Expected: FAIL because routes are missing.

**Step 3: Wire the store into the handler**

Add `EvolutionStore *EvolutionControlStore` and `EvolutionEnabled bool` to `KBaseHTTPConfig`. Derive the default database location from the configured KBase root; do not add a machine-specific fallback path.

**Step 4: Implement read handlers**

Keep routing in `kbase_http.go` and handler bodies in `kbase_http_evolution.go`. Use existing JSON error helpers, authentication, cancellation, pagination, and safe-error conventions.

**Step 5: Regenerate the structural map**

Run:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

Expected: PASS with generated route inventory updated.

**Step 6: Run HTTP regression tests**

Run:

```bash
go test ./backend/app ./cmd/kbase-server -run 'Test.*Evolution|TestKBaseHTTPHandlerListsEmptyAgentPackagesAsArray'
```

Expected: PASS.

**Step 7: Commit**

```text
feat(kbase): expose evolution inbox APIs
```

### Task 6: Replace the Package directory with the dense read-only evolution inbox

**Files:**
- Create: `frontend-web/evolution-console.js`
- Create: `frontend-web/scripts/agent-evolution-console-smoke.mjs`
- Modify: `frontend-web/app.js:312-345`
- Modify: `frontend-web/app.js:4199-4340`
- Modify: `frontend-web/app.js:5340-5500`
- Modify: `frontend-web/app.js:5823-5905`
- Modify: `frontend-web/styles.css:4650-4950`
- Modify: `frontend-web/index.html`

**Step 1: Write failing pure-helper tests**

Test URL parsing and serialization, Package grouping, risk ordering, current-version selection, score deltas, and modified-click behavior. Require this route shape:

```text
/agent-packages?view=inbox&risk=p0,p1&type=combined&run=run-123
```

**Step 2: Write failing UI smoke assertions**

Require Chinese labels and semantic markers for:

- `Agent 演化中心`
- `待审批`
- `已阻断`
- `知识过期`
- `运行异常`
- `演化待办队列`
- `线上版本对比`
- `全部 Agent`
- `演化历史`
- `演化规则`
- `data-evolution-run-id`
- `data-agent-package-id`
- `aria-live="polite"`

Reject the old giant `Agent Packages` index hero and a permanently expanded compiler form.

**Step 3: Verify RED**

Run:

```bash
node frontend-web/scripts/agent-evolution-console-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: the new smoke fails while existing smoke remains green.

**Step 4: Implement the compact view state**

Expose pure helpers through `globalThis.AgentEvolutionConsole`, load the classic helper before `app.js`, and add one `evolutionConsoleState` object. Fetch overview, runs, and selected detail in parallel with route-sequence guards so stale requests cannot replace newer filters.

**Step 5: Implement the desktop and mobile layouts**

Render:

- compact toolbar and status strip;
- prioritized queue on the left;
- selected run comparison on the right;
- one-row-per-Agent fleet table below;
- folded version history;
- explicit empty, loading, unavailable, and failed states.

Use semantic `<button>`, `<a>`, `<table>`, `<dialog>` or an accessible drawer pattern. Preserve visible `:focus-visible`, keyboard operation, long-ID truncation, `tabular-nums`, and `prefers-reduced-motion`.

**Step 6: Move the compiler into a drawer**

Keep current compiler behavior and tests, but render it only after the user activates `创建候选`. Closing the drawer must preserve unsent fields and return focus to the trigger.

**Step 7: Bind URL state and browser history**

Filters, selected run, active view, detail tab, cursor, and drawer state must survive refresh and Back/Forward. Ordinary links must preserve Cmd/Ctrl/middle-click behavior.

**Step 8: Run Layer 1 verification**

Run:

```bash
node frontend-web/scripts/agent-evolution-console-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/browser-cookie-session-smoke.mjs
node frontend-web/scripts/browser-session-settings-smoke.mjs
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: PASS.

**Step 9: Perform browser QA**

Use a temporary non-private fixture to verify desktop density, mobile queue-to-detail navigation, keyboard focus, deep-link refresh, Back/Forward, long identifiers, empty queues, and API failure states. Capture a screenshot for review.

**Step 10: Record G2 and commit**

Update the dossier with Layer 1 feasibility and review results, then commit exact files:

```text
feat(web): add agent evolution inbox
```

### Task 7: Persist immutable candidates and deliver both generation workers

**Files:**
- Create: `backend/app/evolution_candidate.go`
- Test: `backend/app/evolution_candidate_test.go`
- Create: `backend/app/evolution_worker_client.go`
- Test: `backend/app/evolution_worker_client_test.go`
- Create: `backend/app/evolution_generation.go`
- Test: `backend/app/evolution_generation_test.go`
- Create: `cmd/agent-evolution-worker/main.go`
- Test: `cmd/agent-evolution-worker/main_test.go`
- Create: `cmd/knowledge-evolution-worker/main.go`
- Test: `cmd/knowledge-evolution-worker/main_test.go`
- Modify: `backend/app/kbase_http_evolution.go`
- Modify: `backend/app/kbase_http_evolution_test.go`

**Step 1: Write failing immutable-candidate tests**

Prove the same idempotency key and same content return one candidate, changed content conflicts, content hashes bind all runtime-significant fields, and a saved candidate cannot be overwritten.

**Step 2: Implement the candidate artifact store**

Store canonical candidate JSON under a content-addressed evolution artifact directory. The SQLite row stores only identity, type, baseline, generator version, summary, and artifact reference.

**Step 3: Write failing Agent generation adapter tests**

Given a leased Agent run, call existing `CompileAgentPackages` with the pinned primary/support Release identities. Assert the adapter stores ready candidates, records blocked compiler issues, and never evaluates or publishes.

**Step 4: Write failing knowledge generation adapter tests**

Given a knowledge or combined run, reuse existing feedback assessment and reverification contracts. Assert the adapter waits for a candidate-ready immutable Release, records its identity, and never invokes `PublishKnowledgeRelease`.

**Step 5: Implement the shared worker client**

Support heartbeat, lease, renew, complete, fail, and graceful shutdown over authenticated HTTP. Generation and evaluation workers use the existing shared Worker Token; do not add signing or arbitrary command execution.

**Step 6: Implement both worker commands in the same delivery**

Each command supports:

```text
build-info
check-config
run
```

Use bounded polling, signal-aware shutdown, lease renewal, safe error codes, and structured build metadata. Register distinct capabilities in existing Agent management heartbeats.

**Step 7: Verify both workers**

Run:

```bash
go test ./backend/app -run 'TestEvolutionCandidate|TestEvolutionGeneration|TestEvolutionWorkerClient'
go test ./cmd/agent-evolution-worker ./cmd/knowledge-evolution-worker
```

Expected: PASS.

**Step 8: Commit**

```text
feat(agent): generate evolution candidates
```

### Task 8: Add deterministic scorecards and the evaluation worker

**Files:**
- Create: `backend/app/evolution_scorecard.go`
- Test: `backend/app/evolution_scorecard_test.go`
- Create: `backend/app/evolution_evaluation.go`
- Test: `backend/app/evolution_evaluation_test.go`
- Create: `cmd/evaluation-worker/main.go`
- Test: `cmd/evaluation-worker/main_test.go`
- Modify: `backend/app/kbase_http_evolution.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/agent-evolution-console-smoke.mjs`

**Step 1: Write failing scorecard tests**

Use fixed fixtures for the approved weights:

```go
var DefaultEvolutionMetricWeights = map[string]float64{
    "answer_quality": 0.30,
    "evidence_quality": 0.25,
    "task_completion": 0.20,
    "reliability": 0.10,
    "cost": 0.10,
    "latency": 0.05,
}
```

Test a 3-point minimum gain, hard-gate failure despite a higher weighted score, versioned weights, missing metrics, NaN/Inf rejection, and stable rounding.

**Step 2: Implement scorecard calculation**

Persist raw baseline/candidate metrics, normalized values, hard gates, suite/scorer versions, weighted scores, delta, and decision. Never recalculate a saved historical scorecard using newer weights.

**Step 3: Write failing evaluation adapter tests**

Reuse existing trusted Agent evaluation suites, Package evaluation reports, knowledge quality reports, citations, runtime traces, and deterministic test fixtures. For `combined`, require both component reports and explicit contribution fields.

**Step 4: Implement the evaluation worker**

Lease only `evaluation` work. It may save scorecards and transition `evaluating` to `awaiting_approval` or `blocked`; it cannot approve or publish.

**Step 5: Show scorecards in the read-only detail pane**

Render baseline, candidate, delta, hard gates, failure cases, suite version, and artifact identity. Use tabular numbers and explain why a candidate was automatically discarded.

**Step 6: Run Layer 2 verification**

Run:

```bash
go test ./backend/app -run 'TestEvolutionScorecard|TestEvolutionEvaluation'
go test ./cmd/evaluation-worker
node frontend-web/scripts/agent-evolution-console-smoke.mjs
go test -race ./backend/app -run 'TestEvolutionWorker|TestEvolutionEvaluation'
```

Expected: PASS; no publication call is observed.

**Step 7: Update G3 evidence and commit**

```text
feat(agent): evaluate evolution candidates
```

### Task 9: Bind human approvals to candidate and baseline identities

**Files:**
- Create: `backend/app/evolution_approval.go`
- Test: `backend/app/evolution_approval_test.go`
- Modify: `backend/app/kbase_http_evolution.go`
- Modify: `backend/app/kbase_http_evolution_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/agent-evolution-console-smoke.mjs`

**Step 1: Write failing approval tests**

Cover valid approval, rejection, request-regeneration, defer, candidate-hash mismatch, baseline drift, expired approval, failed hard gates, missing scorecard, duplicate decision replay, and changed payload under the same idempotency key.

**Step 2: Implement approval validation**

Add `ApproveEvolutionRun`, `RejectEvolutionRun`, `RequestEvolutionRegeneration`, and `DeferEvolutionRun`. Approval records bind `candidate_id`, candidate content hash, baseline identity, scorecard ID, approver, decision, reason code, and expiration.

**Step 3: Add authenticated action endpoints**

Implement:

```text
POST /api/evolution/runs/:run_id/approve
POST /api/evolution/runs/:run_id/reject
POST /api/evolution/runs/:run_id/retry
```

Use the existing browser/admin authorization boundary. Worker credentials must not be accepted for human approval actions.

**Step 4: Implement approval UI**

Keep approval actions in a sticky detail footer. Confirmation displays exact candidate version/hash, baseline, affected Agent/Release, risk, and rollback identity. Rejection and regeneration require a bounded reason code plus optional bounded note.

**Step 5: Verify accessibility and double-submit protection**

Focus the first invalid field, keep the submit button enabled until submission starts, announce results through `aria-live`, and reject duplicate active requests in both UI and API.

**Step 6: Run tests and commit**

Run:

```bash
go test ./backend/app -run 'TestEvolutionApproval|TestKBaseHTTP.*Evolution'
node frontend-web/scripts/agent-evolution-console-smoke.mjs
```

Then commit:

```text
feat(kbase): approve evolution releases
```

### Task 10: Implement restricted publication, observation entry, and rollback

**Files:**
- Create: `backend/app/evolution_release.go`
- Test: `backend/app/evolution_release_test.go`
- Modify: `backend/app/agent_package_store.go`
- Modify: `backend/app/agent_package_store_test.go`
- Modify: `backend/app/knowledge_release.go`
- Modify: `backend/app/knowledge_release_test.go`
- Create: `cmd/evolution-release-worker/main.go`
- Test: `cmd/evolution-release-worker/main_test.go`
- Modify: `backend/app/kbase_http_evolution.go`
- Modify: `backend/app/kbase_http_evolution_test.go`

**Step 1: Write failing publication-gate tests**

Assert publication fails for missing/expired approval, changed candidate hash, changed baseline, changed suite version, failed hard gate, or a stale Worker lease. Replayed publication must return the original result without a second Package/Release publication.

**Step 2: Implement the publication plan**

Before side effects, persist a deterministic publication plan containing approved identities, ordered steps, idempotency keys, previous serving identities, and rollback targets.

For `combined` runs:

1. publish the immutable Knowledge Release candidate;
2. publish the Agent Package pinned to that exact Release;
3. leave the previous Agent Package serving until step 2 succeeds;
4. if step 2 fails, mark the run blocked while the old Agent remains serving.

Do not delete a partially published immutable Knowledge Release.

**Step 3: Add explicit rollback primitives**

Implement a manifest operation that re-promotes the approved previous Agent Package identity without mutating its artifact. Record the rollback as a new control-plane event. Reject rollback to missing, invalid, or policy-incompatible artifacts.

**Step 4: Implement the release worker**

The release worker uses the existing publisher credential for existing Package publication APIs and the shared Worker Token for evolution lease/report APIs. It cannot approve. Support `build-info`, `check-config`, and `run`.

**Step 5: Implement observation entry**

Successful publication transitions to `observing`, saves the new and previous serving identities, and creates bounded observation work. It does not mark the run completed immediately.

**Step 6: Run Layer 3 tests**

Run:

```bash
go test ./backend/app -run 'TestEvolutionRelease|TestAgentPackageStore.*Rollback|TestKnowledgeRelease'
go test ./cmd/evolution-release-worker
go test -race ./backend/app -run 'TestEvolutionRelease'
```

Expected: PASS.

**Step 7: Update G4 evidence and commit**

```text
feat(kbase): publish approved evolution versions
```

### Task 11: Close the loop with bounded production observations

**Files:**
- Create: `backend/app/evolution_observation.go`
- Test: `backend/app/evolution_observation_test.go`
- Modify: `backend/app/agent_trace.go`
- Modify: `backend/app/knowledge_feedback.go`
- Modify: `backend/app/evidence_audit_store.go`
- Modify: `backend/app/evolution_worker.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/agent-evolution-console-smoke.mjs`

**Step 1: Write failing observation tests**

Use fixed Agent traces, evidence audits, knowledge feedback, and runtime failures. Test:

- improvement completes the run after the observation window;
- no significant gain remains observing until the bounded window ends, then completes with `no_material_gain`;
- hard-gate regression creates rollback work immediately;
- stale/incorrect feedback creates a new deduplicated knowledge signal;
- rollback creates a new signal but never publishes a replacement automatically.

**Step 2: Implement bounded metric aggregation**

Read only allowlisted aggregate fields from existing traces, audits, and feedback. Do not copy prompts, answers, downloaded text, or free-form sensitive feedback into the evolution database.

**Step 3: Implement observation decisions**

Compare production observation metrics to the published scorecard and previous baseline. Persist the decision and exact source identities. Hard-gate incidents enqueue release rollback work; ordinary regressions create new signals.

**Step 4: Add history and trend views**

Show why the system changed, what changed, expected result, observed result, rollback status, and linked immutable identities. Use `Intl.DateTimeFormat` and `Intl.NumberFormat` in the browser.

**Step 5: Verify the closed loop**

Run:

```bash
go test ./backend/app -run 'TestEvolutionObservation|TestEvolutionSignal'
node frontend-web/scripts/agent-evolution-console-smoke.mjs
```

Expected: PASS.

**Step 6: Commit**

```text
feat(kbase): observe evolution outcomes
```

### Task 12: Deliver workers, management visibility, feature flags, and rollback assets

**Files:**
- Create: `deploy/systemd/dedao-agent-evolution-worker.service`
- Create: `deploy/systemd/dedao-knowledge-evolution-worker.service`
- Create: `deploy/systemd/dedao-evaluation-worker.service`
- Create: `deploy/systemd/dedao-evolution-release-worker.service`
- Modify: `scripts/kbase-direct-deployment-smoke.sh`
- Modify: `scripts/kbase-direct-deployment-behavior-smoke.sh`
- Modify: `scripts/kbase-direct-deployment-cutover.sh`
- Modify: `scripts/testdata/kbase-direct-deployment/mock-command.sh`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/source-agent-control-plane-smoke.mjs`
- Modify: `README.md`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

**Step 1: Write failing deployment smoke assertions**

Require all worker binaries, `check-config`, systemd units, shared-token configuration, disabled-by-default evolution feature flag, health probes, backup, cutover ordering, and rollback restoration.

**Step 2: Add Worker management projections**

Register all evolution workers in `/sources/agents` with capability, revision, platform, current run/stage, last heartbeat, and bounded last error. Preserve the existing “diagnose and restricted restart only” command boundary.

**Step 3: Add shadow-mode feature flags**

Support:

```text
KBASE_EVOLUTION_ENABLED=0|1
KBASE_EVOLUTION_GENERATION_ENABLED=0|1
KBASE_EVOLUTION_PUBLICATION_ENABLED=0|1
```

Default all new mutation paths to disabled. Layer 1 can be enabled read-only. Generation and publication require explicit independent enablement.

**Step 4: Extend direct deployment**

Build and stage all binaries before stopping services. Back up binaries, units, evolution database, Package manifest, and knowledge metadata. Start control plane before workers; start release worker last. On any health failure, restore previous binaries/units and leave new database artifacts recoverable.

**Step 5: Add rollback documentation**

Document how to disable publication, stop workers, restore services, re-promote the previous Agent Package, and retain immutable candidates/events for diagnosis. Do not include real hosts, tokens, usernames, or absolute local paths.

**Step 6: Run deployment tests**

Run:

```bash
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
go test ./cmd/agent-evolution-worker ./cmd/knowledge-evolution-worker ./cmd/evaluation-worker ./cmd/evolution-release-worker ./cmd/kbase-server
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
```

Expected: PASS.

**Step 7: Commit**

```text
feat(delivery): ship evolution workers
```

### Task 13: Run end-to-end gates and prepare staged rollout

**Files:**
- Create: `backend/app/evolution_e2e_test.go`
- Modify: `frontend-web/scripts/agent-evolution-console-smoke.mjs`
- Modify: `docs/dossiers/2026-08-11-agent-evolution-control-plane.md`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Add three deterministic end-to-end scenarios**

Cover:

1. Agent signal → Agent candidate → evaluation → human approval → publication → successful observation.
2. stale knowledge signal → reverification candidate → evaluation → human approval → Knowledge Release publication.
3. missing evidence → combined candidate → component scorecards → approval → ordered publication → hard-gate incident → automatic rollback.

Each scenario must assert event order, immutable identities, approval binding, idempotency, and no automatic re-publication.

**Step 2: Add failure-injection scenarios**

Cover Worker crash, expired lease, retry exhaustion, outbox failure, candidate tampering, stale approval, baseline drift, partial combined publication, observation timeout, and rollback replay.

**Step 3: Run narrow verification first**

Run:

```bash
go test ./backend/app -run 'TestEvolutionEndToEnd|TestEvolution'
go test -race ./backend/app -run 'TestEvolution'
node frontend-web/scripts/agent-evolution-console-smoke.mjs
```

Expected: PASS.

**Step 4: Run complete release verification**

Run without piping output:

```bash
for script in frontend-web/scripts/*smoke.mjs; do node "$script"; done
cd frontend && npm run build
cd ..
go test ./...
go vet ./...
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: every command exits 0; status contains only intended feature, generated map, dossier, and plan files.

**Step 5: Perform final browser acceptance**

Verify on non-private fixtures:

- highest-priority work is visible without scrolling;
- one row per Agent with folded versions;
- queue/detail deep links and Back/Forward;
- Agent, knowledge, and combined task timelines;
- automatic candidate discard explanation;
- stale approval invalidation;
- publication confirmation and observation state;
- rollback event and continued manual publication gate;
- desktop/mobile layouts, keyboard navigation, long content, and empty/error states;
- no console errors or warnings caused by the feature.

**Step 6: Request independent review**

Review domain transitions, SQL transactions, API authorization, shared-token boundary, Worker lease correctness, candidate/approval identity binding, publication idempotency, rollback safety, privacy, accessibility, and deployment rollback.

**Step 7: Record G3–G6 decisions**

Update the dossier with exact test evidence, review findings, deployment health, production observation, and any rejected gate. Never mark a gate passed from expected output alone.

**Step 8: Commit final evidence**

Stage only intended files and commit:

```text
docs(kbase): record evolution rollout gates
```

## Execution handoff

Recommended execution order is strictly sequential by acceptance layer. Tasks inside a layer may use subagents only when they edit disjoint files; shared state-machine, store, handler, and `frontend-web/app.js` work must remain serialized.
