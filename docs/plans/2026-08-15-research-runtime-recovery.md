# Research Runtime Recovery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Recover once from structurally invalid role-model output, keep the repair durable and budgeted, and distinguish real Worker outages from connected Worker/query failures.

**Architecture:** Extend the existing Research invocation ledger with a bounded failure category, then route a failed primary role invocation to one separately identified repair invocation. Keep all calls behind the existing lease, model-call, and cost reservations. Classify expired Worker jobs as offline and explicit terminal failures as connected Worker failures. Raise the fixed loopback Chatlog deadline from ten to thirty seconds because the production query completed after the former client deadline.

**Tech Stack:** Go 1.21+, SQLite, existing Research coordinator/orchestrator, macOS Chatlog Worker, static JavaScript frontend, shell/process smoke tests.

---

### Task 1: Fix the reproduced long-range Chatlog timeout

**Files:**
- Modify: `backend/app/chatlog_http.go`
- Test: `backend/app/chatlog_http_test.go`

**Step 1: Write the failing timeout-contract test**

Add a test in package `app` that constructs a reader without a custom HTTP
client and asserts the fixed default deadline:

```go
func TestChatlogHTTPDefaultTimeoutSupportsBoundedLongLocalQueries(t *testing.T) {
    reader, err := NewChatlogHTTPReader(ChatlogHTTPConfig{BaseURL: "http://127.0.0.1:5030"})
    if err != nil { t.Fatal(err) }
    if reader.client.Timeout != 30*time.Second {
        t.Fatalf("timeout=%s want=30s", reader.client.Timeout)
    }
}
```

Keep the existing short custom-client timeout test so callers can still impose
a smaller deadline and the constructor still caps values above thirty seconds.

**Step 2: Run the test and verify RED**

Run:

```bash
go test ./backend/app -run 'TestChatlogHTTP(DefaultTimeout|EnforcesTimeout)' -count=1
```

Expected: FAIL because the default is currently ten seconds.

**Step 3: Implement the minimal deadline change**

Introduce a named `defaultChatlogHTTPTimeout = 30 * time.Second` constant and
use it both when no client is supplied and when a zero or over-limit client
timeout is normalized. Do not add a job-controlled timeout.

**Step 4: Verify GREEN**

Run the command from Step 2 plus:

```bash
go test ./backend/app ./cmd/chatlog-agent -run 'TestChatlogHTTP|TestChatlogAgent' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/chatlog_http.go backend/app/chatlog_http_test.go
git commit -m "fix(chatlog): allow bounded long local queries"
```

### Task 2: Separate Worker offline and Worker execution failure

**Files:**
- Modify: `backend/app/research_orchestrator.go`
- Test: `backend/app/research_orchestrator_test.go`
- Modify: `frontend-web/app.js`
- Test: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write failing backend classification tests**

Add tests proving:

```go
failed := ResearchWorkerJob{State: ResearchWorkerJobFailed, FailureCode: "dependency_unavailable"}
expired := ResearchWorkerJob{State: ResearchWorkerJobExpired}
```

An orchestrator encountering `failed` must finish as `worker_failed`; an
expired job must finish as `worker_offline`. Also assert invalid persisted
candidate boundaries classify as `worker_failed`, not offline.

**Step 2: Run the backend tests and verify RED**

```bash
go test ./backend/app -run 'TestResearchOrchestrator.*Worker.*(Failed|Offline|Outcome)' -count=1
```

Expected: FAIL because both paths currently use `ErrResearchWorkerTerminal`.

**Step 3: Implement the minimal typed outcome split**

Add:

```go
const ResearchOutcomeWorkerFailed = "worker_failed"
var ErrResearchWorkerFailed = errors.New(ResearchOutcomeWorkerFailed)
```

In retrieval, return `ErrResearchWorkerFailed` for an explicit failed job and
retain `ErrResearchWorkerTerminal` for an expired job. Use the failed sentinel
for invalid persisted candidate/result boundaries. Extend
`ClassifyResearchOrchestratorOutcome` without changing lease or retry rules.

**Step 4: Add the failing frontend presentation assertion**

Extend the Web smoke to require `worker_failed` and its Chinese explanation,
while retaining the legacy `worker_offline` text.

**Step 5: Implement the frontend presentation**

Map `worker_failed` to a message stating that the Worker connected but its
local query or returned data failed, and direct the operator to Agent health.

**Step 6: Verify backend and frontend GREEN**

```bash
go test ./backend/app -run 'TestResearchOrchestrator.*Worker|TestResearchOrchestratorTypedOutcomes' -count=1
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

**Step 7: Commit**

```bash
git add backend/app/research_orchestrator.go backend/app/research_orchestrator_test.go frontend-web/app.js frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "fix(research): report connected worker failures"
```

### Task 3: Persist bounded model validation failures

**Files:**
- Modify: `backend/app/research_store.go`
- Modify: `backend/app/research_orchestrator.go`
- Test: `backend/app/research_orchestrator_test.go`
- Test: `backend/app/research_store_test.go`

**Step 1: Write failing migration and ledger tests**

Assert `research_model_invocations` migrates a
`failure_code TEXT NOT NULL DEFAULT ''` column. Add a focused invocation test
where the fake stage model returns `ErrResearchInvalidModelOutput` and verify
the row is `failed` with `failure_code=invalid_model_output`.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestResearch(ModelInvocationFailure|StoreMigration.*Model)' -count=1
```

Expected: FAIL because no bounded failure code is stored.

**Step 3: Implement the migration and allowlisted failure metadata**

Use `ensureResearchStoreColumn` for the new column. Add a helper that maps only
known errors to fixed values:

```go
func researchModelFailureCode(err error) string {
    switch {
    case errors.Is(err, ErrResearchInvalidModelOutput): return ResearchOutcomeInvalidModelOutput
    case errors.Is(err, context.DeadlineExceeded): return ResearchOutcomeModelTimeout
    default: return "model_error"
    }
}
```

Update the same fenced failure transaction that marks the invocation failed;
do not persist raw provider output or raw validation messages.

**Step 4: Verify GREEN**

Run the command from Step 2 and expect PASS.

**Step 5: Commit**

```bash
git add backend/app/research_store.go backend/app/research_store_test.go backend/app/research_orchestrator.go backend/app/research_orchestrator_test.go
git commit -m "fix(research): persist model failure categories"
```

### Task 4: Add one durable, budgeted repair invocation

**Files:**
- Modify: `backend/app/research_orchestrator.go`
- Test: `backend/app/research_orchestrator_test.go`
- Modify: `backend/app/research_model.go`
- Test: `backend/app/research_model_test.go`

**Step 1: Write failing recovery tests**

Extend the fake model so the first extractor call can be invalid and the second
valid. Prove:

1. exactly two extractor calls occur;
2. both calls increment `model_calls` and have distinct request identities;
3. a successful repair continues to synthesis;
4. two invalid outputs terminate as `extractor_invalid_output` with no third
   call;
5. an existing failed primary row resumes directly at repair;
6. insufficient model-call or cost budget prevents the repair;
7. timeouts and ordinary provider failures do not trigger repair.

**Step 2: Run the focused tests and verify RED**

```bash
go test ./backend/app -run 'TestResearchOrchestrator.*(Repair|InvalidOutput|RepairBudget|RepairResume)' -count=1
```

Expected: FAIL because the primary invalid output currently terminates.

**Step 3: Implement durable repair routing**

Factor request identity construction so the wrapper can inspect the primary
invocation. Add `invokeModelWithRepair` with this flow:

```text
primary cached success -> return
primary failed invalid -> skip primary provider call
primary absent/transient -> invoke primary
primary invalid -> append fixed repair instruction
invoke requestKey + ":repair:1" through existing invokeModel
repair invalid -> return role-specific invalid-output sentinel
```

The repair instruction contains only a fixed validation category and the role
schema. It reuses the original messages and requires regeneration of the full
JSON object. It must not include raw error text or relax runtime reference
validation.

Replace the four stage calls with the wrapper. Add fixed role-specific outcome
constants and classification while retaining legacy
`invalid_model_output` compatibility.

**Step 4: Verify GREEN and durable accounting**

Run the command from Step 2 plus:

```bash
go test -race ./backend/app -run 'TestResearchOrchestrator.*(Repair|Model|Coordinator)' -count=1
```

Expected: PASS with exactly the asserted call and reservation counts.

**Step 5: Commit**

```bash
git add backend/app/research_model.go backend/app/research_model_test.go backend/app/research_orchestrator.go backend/app/research_orchestrator_test.go
git commit -m "fix(research): repair invalid role output once"
```

### Task 5: Present role-specific model failures in Chinese

**Files:**
- Modify: `frontend-web/app.js`
- Test: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write the failing Web smoke assertions**

Require all four role-specific codes and Chinese labels that identify planning,
fact extraction, synthesis, or verification. Keep the legacy generic mapping.

**Step 2: Verify RED**

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: FAIL because the new codes are absent.

**Step 3: Implement the fixed presentation map**

Add only allowlisted mappings; do not display private failure messages.

**Step 4: Verify GREEN**

```bash
node --check frontend-web/app.js
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "fix(web): explain research recovery failures"
```

### Task 6: Run release gates and production acceptance

**Files:**
- Modify: `docs/dossiers/2026-08-13-research-agent-platform.md`
- Regenerate if structurally required: `docs/_generated/system-map.json`

**Step 1: Run focused and complete verification**

```bash
go test ./backend/app ./cmd/chatlog-agent ./cmd/kbase-server -count=1
go test -race ./backend/app ./cmd/chatlog-agent -count=1 -run 'TestResearch|TestChatlog'
go vet ./...
go test ./... -timeout=600s -count=1
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
bash scripts/research-agent-smoke.sh
bash scripts/chatlog-agent-packaging-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: every command exits zero without output truncation or a piped-away
failure status.

**Step 2: Update generated structure only when required**

If the system-map smoke reports drift, regenerate with the documented generator
and rerun the smoke. Do not hand-edit generated counts.

**Step 3: Record privacy-safe evidence**

Append only revision, run/job IDs, states, bounded counts, durations, outcomes,
and gate results. Do not record query strings, message bodies, identities,
locators, credentials, or local paths.

**Step 4: Commit the gate record**

```bash
git add docs/dossiers/2026-08-13-research-agent-platform.md docs/_generated/system-map.json
git commit -m "docs(research): record runtime recovery gates"
```

**Step 5: Merge, deploy, and verify exact revision**

Follow the existing clean-main direct-deployment contract. Deploy both KBase
and the macOS Chatlog Worker built from the same exact revision, verify public
and loopback health, installed Worker build identity, service restart counts,
and privacy-safe logs.

**Step 6: Repeat the production case**

Create a new deep run using the same package, knowledge plus Chatlog sources,
and the same research intent. Verify the long-range Chatlog job completes or is
reported as `worker_failed` with an accurate cause, a single invalid role output
can repair once, and the run reaches human identity confirmation or a verified
report. Leave the authenticated browser on the resulting dossier for manual
evaluation.
