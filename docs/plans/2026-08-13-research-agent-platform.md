# Research Agent Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a durable dual-mode Research Agent runtime that can combine versioned knowledge packages with a local read-only Chatlog Worker, reconstruct timelines, compare cases, detect conflicts, and publish a citation-verified report.

**Architecture:** Extend the existing KBase control plane with a SQLite-backed Research Run state machine and one role-separated orchestrator. Keep complete Chatlog data local behind an outbound-polling macOS Worker that uses the existing shared Worker token; the server stores typed run state, opaque locators and hashes, and only the minimal evidence excerpts promoted into the report.

**Tech Stack:** Go, SQLite, existing KBase HTTP server, TokenPlan-compatible chat completions, local Chatlog HTTP API, macOS launchd, vanilla JavaScript, semantic HTML/CSS, Node smoke tests, direct KBase deployment.

---

## Execution rules

- Start implementation in a dedicated `codex/` worktree with
  `@using-git-worktrees`; do not implement in the current dirty `main` worktree.
- Use `@test-driven-development` for every task: RED, minimal GREEN, refactor,
  then a scoped commit.
- Use `@systematic-debugging` for Chatlog, model, Worker, browser, or deployment
  failures. Do not replace a failed dependency with a silent fallback.
- Use `.claude/skills/privacy-guard/SKILL.md` before changing configuration,
  paths, fixtures, docs, packaging, commits, or publishing surfaces.
- Use `@requesting-code-review` at each delivery-layer gate and
  `@verification-before-completion` before any completion, merge, or deployment
  claim.
- Stage only the files named by the current task. Never use `git add -A`.
- Do not introduce request signing. Worker endpoints continue to use the
  existing shared Worker token and Bearer authentication.
- Do not persist a raw Chatlog response body, full contact list, complete
  conversation, credential, cookie, private filesystem path, or unbounded
  model output.
- Do not expose model chain-of-thought. Persist bounded stage summaries,
  structured decisions, evidence references, and verification results.

## Feasibility checkpoint

A macOS feasibility probe confirmed that the local Chatlog HTTP API can answer
an authenticated-locality-independent read request on loopback. A separate
probe found that launching a mismatched x86_64 Chatlog executable on an arm64
host can crash during process initialization. The implementation must therefore:

1. query the loopback HTTP API instead of shelling out once per tool call;
2. fail closed when the local API is unavailable;
3. reject non-loopback Chatlog endpoints and redirects;
4. validate native architecture during Worker installation and upgrade; and
5. never claim Worker readiness from process presence alone.

## Delivery layers and gates

Stop after each layer for review. A failed gate returns to the responsible task.

1. **Layer A — contracts and persistence:** Tasks 1–3.
2. **Layer B — local Chatlog Worker:** Tasks 4–6.
3. **Layer C — retrieval and orchestration:** Tasks 7–10.
4. **Layer D — versioned Agent and operator UX:** Tasks 11–12.
5. **Layer E — evaluation, release, and production proof:** Tasks 13–14.

No later layer may inherit a prior PASS without rerunning its applicable G1–G6
checks. In particular, local fixture success is not production evidence.

### Task 1: Create the dossier, domain contract, and dual-mode router

**Files:**
- Create: `docs/dossiers/2026-08-13-research-agent-platform.md`
- Create: `backend/app/research_run.go`
- Test: `backend/app/research_run_test.go`

**Step 1: Create the dossier skeleton**

Record the approved design, this implementation plan, the five delivery layers,
the privacy boundary, G1 as pending until the contract tests pass, and G2–G6 as
pending. Use generic case labels and synthetic identities only.

**Step 2: Write failing status and router tests**

Cover all approved states and terminal behavior. Explicit mode wins; `auto`
routes to deep for Chatlog, identity, history, cross-time, comparison, or
conflict requirements and otherwise routes to quick.

```go
func TestRouteResearchModeChoosesDeepForPrivateHistory(t *testing.T) {
	request := ResearchRunRequest{
		Mode:             ResearchModeAuto,
		Question:         "Compare the current case with an earlier case.",
		RequestedSources: []string{ResearchSourceKnowledge, ResearchSourceChatlog},
	}
	mode, reasons, err := RouteResearchMode(request)
	if err != nil || mode != ResearchModeDeep || !slices.Contains(reasons, "private_history") {
		t.Fatalf("mode=%q reasons=%v err=%v", mode, reasons, err)
	}
}
```

Also test that a request cannot jump from `planning` to `completed`, terminal
runs cannot resume, and quick mode returns `deep_research_required` rather than
silently escalating.

**Step 3: Verify RED**

Run: `go test ./backend/app -run 'TestResearchRun|TestRouteResearchMode'`

Expected: FAIL because the research types and router do not exist.

**Step 4: Implement the minimal public contract**

Define bounded, JSON-safe records:

```go
type ResearchRunStatus string

const (
	ResearchPlanning            ResearchRunStatus = "planning"
	ResearchRetrieving          ResearchRunStatus = "retrieving"
	ResearchResolvingIdentity   ResearchRunStatus = "resolving_identity"
	ResearchBuildingTimeline    ResearchRunStatus = "building_timeline"
	ResearchExtractingFacts     ResearchRunStatus = "extracting_facts"
	ResearchDetectingConflicts  ResearchRunStatus = "detecting_conflicts"
	ResearchComparingCases      ResearchRunStatus = "comparing_cases"
	ResearchSynthesizing        ResearchRunStatus = "synthesizing"
	ResearchVerifying           ResearchRunStatus = "verifying"
	ResearchCompleted           ResearchRunStatus = "completed"
	ResearchInsufficient        ResearchRunStatus = "insufficient"
	ResearchFailed              ResearchRunStatus = "failed"
	ResearchCanceled            ResearchRunStatus = "canceled"
)

type ResearchRunRequest struct {
	Mode             string   `json:"mode"`
	Question         string   `json:"question"`
	PackageID        string   `json:"package_id"`
	PackageVersion   string   `json:"package_version"`
	RequestedSources []string `json:"requested_sources"`
}
```

Add `ResearchRun`, `ResearchScope`, `ResearchStageSummary`, `ResearchBudget`,
`ResearchFailure`, `ValidateResearchTransition`, and `RouteResearchMode`.
Bound question length, source count, subject count, iteration count, evidence
count, total excerpt characters, model calls, and estimated cost.

**Step 5: Run focused tests**

Run: `go test ./backend/app -run 'TestResearchRun|TestRouteResearchMode'`

Expected: PASS.

**Step 6: Commit**

```bash
git add docs/dossiers/2026-08-13-research-agent-platform.md backend/app/research_run.go backend/app/research_run_test.go
git commit -m "feat(kbase): define research run contract"
```

### Task 2: Build the transactional Research Store

**Files:**
- Create: `backend/app/research_store.go`
- Test: `backend/app/research_store_test.go`
- Modify: `backend/app/research_run.go`

**Step 1: Write failing migration and transaction tests**

Test create/reopen, idempotent request creation, optimistic transition version,
atomic event append, cursor ordering, expired lease recovery, and terminal-run
immutability. Add a fault hook proving a failed event insertion rolls back the
run update.

```go
func TestResearchStoreTransitionsRunAndEventAtomically(t *testing.T) {
	store := newResearchTestStore(t)
	run := createResearchTestRun(t, store)
	updated, err := store.TransitionRun(run.RunID, run.Version, ResearchRetrieving,
		ResearchTransition{Code: "plan_ready", Actor: "orchestrator"})
	if err != nil || updated.Status != ResearchRetrieving {
		t.Fatalf("run=%#v err=%v", updated, err)
	}
	events := mustListResearchEvents(t, store, run.RunID)
	if len(events) != 2 || events[1].ToStatus != ResearchRetrieving {
		t.Fatalf("events=%#v", events)
	}
}
```

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestResearchStore'`

Expected: FAIL because `ResearchStore` does not exist.

**Step 3: Implement schema version 1**

Create `research_control.sqlite3` under the configured KBase root with:

```sql
CREATE TABLE research_runs (...);
CREATE TABLE research_events (...);
CREATE TABLE research_steps (...);
CREATE TABLE research_evidence (...);
CREATE TABLE research_identity_bindings (...);
CREATE TABLE research_timeline_events (...);
CREATE TABLE research_claims (...);
CREATE TABLE research_conflicts (...);
CREATE TABLE research_conclusions (...);
CREATE TABLE research_worker_jobs (...);
CREATE TABLE research_model_invocations (...);
CREATE TABLE research_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

Follow the SQLite connection, `busy_timeout`, foreign-key, single-writer, and
transaction patterns in `backend/app/source_sync.go`. Index run status/update
time, event sequence, evidence hash, locator hash, job state/lease, and model
request identity. Do not store Worker result JSON as a blob.

**Step 4: Implement store primitives**

Add:

```go
func OpenResearchStore(root string, now func() time.Time) (*ResearchStore, error)
func (s *ResearchStore) CreateRun(input ResearchRunInput) (*ResearchRun, bool, error)
func (s *ResearchStore) LoadRun(runID string) (*ResearchRun, error)
func (s *ResearchStore) TransitionRun(runID string, version int64, to ResearchRunStatus, input ResearchTransition) (*ResearchRun, error)
func (s *ResearchStore) ListEvents(runID string, after int64, limit int) ([]ResearchEvent, error)
func (s *ResearchStore) ClaimRunnableRun(owner string, lease time.Duration) (*ResearchRun, error)
func (s *ResearchStore) RenewRunLease(runID, owner string, lease time.Duration) error
```

Use content-derived idempotency keys for requests and model invocations. Every
mutation must append a bounded event in the same transaction.

**Step 5: Verify persistence and races**

Run:

```bash
go test ./backend/app -run 'TestResearchStore'
go test -race ./backend/app -run 'TestResearchStore'
```

Expected: PASS with no race report.

**Step 6: Commit**

```bash
git add backend/app/research_run.go backend/app/research_store.go backend/app/research_store_test.go
git commit -m "feat(kbase): persist research runs"
```

### Task 3: Enforce the evidence workspace and privacy boundary

**Files:**
- Create: `backend/app/research_evidence.go`
- Test: `backend/app/research_evidence_test.go`
- Modify: `backend/app/research_store.go`

**Step 1: Write failing evidence normalization tests**

Cover deterministic IDs, deduplication by source locator plus content hash,
source-role validation, excerpt limits, typed privacy levels, derived-evidence
restrictions, searched-versus-cited scope, and source-change detection.

Add sentinels proving raw response bodies, full contact objects, cookies,
Bearer values, local paths, and unselected long message content never reach the
database or public JSON projection.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestResearchEvidence'`

Expected: FAIL because evidence normalization is absent.

**Step 3: Implement evidence contracts**

```go
type ResearchEvidence struct {
	EvidenceID        string                 `json:"evidence_id"`
	SourceType        string                 `json:"source_type"`
	SourceRole        string                 `json:"source_role"`
	AuthorIdentityID  string                 `json:"author_identity_id,omitempty"`
	SubjectIdentityIDs []string              `json:"subject_identity_ids,omitempty"`
	OccurredAt        string                 `json:"occurred_at,omitempty"`
	ContentExcerpt    string                 `json:"content_excerpt,omitempty"`
	Locator           ResearchEvidenceLocator `json:"locator"`
	ContentHash       string                 `json:"content_hash"`
	Privacy           string                 `json:"privacy"`
	Selected          bool                   `json:"selected"`
}
```

Use opaque HMAC-free SHA-256 fingerprints for arguments and locators; these are
identifiers, not an authentication signature. Excerpts must be Unicode-safe,
bounded, and stored only after promotion by the evidence selector.

**Step 4: Add an in-memory Worker result projection**

Implement `NormalizeResearchWorkerResult` so the HTTP handler can validate a
bounded Worker response, derive locator/hash metadata, promote selected minimal
excerpts, and discard the raw decoded response before returning. Never marshal
the raw result into a store method, log event, or error.

**Step 5: Run focused and privacy tests**

Run:

```bash
go test ./backend/app -run 'TestResearchEvidence'
bash scripts/privacy-smoke.sh
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/research_evidence.go backend/app/research_evidence_test.go backend/app/research_store.go
git commit -m "feat(kbase): bound research evidence"
```

### Task 4: Add the shared-token Research Worker protocol

**Files:**
- Create: `backend/app/research_worker.go`
- Create: `backend/app/research_worker_client.go`
- Test: `backend/app/research_worker_test.go`
- Test: `backend/app/research_worker_client_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing job lifecycle tests**

Cover job kinds `search_chatlog`, `expand_chat_context`,
`resolve_chat_identity`, `list_identity_conversations`, and
`fetch_chat_message`; queued/leased/completed/failed/expired states; lease
ownership; renewal; idempotent completion; stale result rejection; retry
budget; and completion-result privacy projection.

**Step 2: Write failing HTTP authentication tests**

Assert:

- anonymous and KBase browser/admin credentials receive 401;
- the existing shared Worker token receives 200;
- no signature header is required or accepted as an auth substitute;
- a disabled Research Store receives 503;
- unknown fields, oversized bodies, invalid time ranges, or over-limit queries
  receive a bounded 400/413 response; and
- one Worker cannot complete another Worker's lease.

**Step 3: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestResearchWorker'
go test ./backend/app -run 'TestKBaseHTTP.*ResearchWorker'
```

Expected: FAIL because the protocol is missing.

**Step 4: Implement the durable job contract**

```go
type ResearchWorkerJob struct {
	JobID          string          `json:"job_id"`
	RunID          string          `json:"run_id"`
	Tool           string          `json:"tool"`
	Arguments      json.RawMessage `json:"arguments"`
	State          string          `json:"state"`
	Attempt        int             `json:"attempt"`
	LeaseOwner     string          `json:"lease_owner,omitempty"`
	LeaseExpiresAt string          `json:"lease_expires_at,omitempty"`
	RequestHash    string          `json:"request_hash"`
}
```

Add transactional create/claim/renew/complete/fail/recover methods to
`ResearchStore`. Keep arguments typed and bounded; do not reuse the 2 KiB
maintenance-command payload as a data channel.

**Step 5: Add Worker routes before normal KBase API auth**

Add the following under the existing Worker Bearer-token branch:

```text
POST /api/research-worker/jobs/claim
POST /api/research-worker/jobs/{job_id}/renew
POST /api/research-worker/jobs/{job_id}/complete
POST /api/research-worker/jobs/{job_id}/fail
```

Use `SourceAgentToken` and `authorizeBearerToken`; do not add a signing key,
nonce signature, cookie session, or a second Worker secret.

**Step 6: Implement the remote client**

Add strict response decoding, body limits, ownership checks, retryable transport
errors, and idempotency headers in `research_worker_client.go`. Keep the
existing `SourceAgentClient` for heartbeat, diagnostics, and upgrade commands.

**Step 7: Run focused tests**

Run:

```bash
go test ./backend/app -run 'TestResearchWorker|TestKBaseHTTP.*ResearchWorker'
go test -race ./backend/app -run 'TestResearchWorker'
```

Expected: PASS.

**Step 8: Commit**

```bash
git add backend/app/research_worker.go backend/app/research_worker_client.go backend/app/research_worker_test.go backend/app/research_worker_client_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): add research worker protocol"
```

### Task 5: Implement the loopback Chatlog adapter and macOS Worker

**Files:**
- Create: `backend/app/chatlog_http.go`
- Test: `backend/app/chatlog_http_test.go`
- Create: `cmd/chatlog-agent/main.go`
- Test: `cmd/chatlog-agent/main_test.go`

**Step 1: Write failing Chatlog client tests**

Use `httptest.Server` fixtures matching Chatlog v0.0.15 JSON fields. Cover:

- `/api/v1/chatlog?format=json` with time, talker, sender, keyword, limit,
  and offset;
- `/api/v1/contact`, `/api/v1/chatroom`, and `/api/v1/session` pagination;
- the mandatory two-stage search-then-context behavior;
- quoted/referred message normalization;
- result ordering and stable `seq` locators;
- response/body/time/row limits;
- non-loopback URL and redirect rejection; and
- timeout, malformed JSON, and unavailable-service errors.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestChatlogHTTP'`

Expected: FAIL because the adapter does not exist.

**Step 3: Implement the read-only adapter**

```go
type ChatlogQuery struct {
	Time    string
	Talker  string
	Sender  string
	Keyword string
	Limit   int
	Offset  int
}

type ChatlogReader interface {
	SearchMessages(context.Context, ChatlogQuery) ([]ChatlogMessage, error)
	ListContacts(context.Context, string, int, int) (ChatlogContactPage, error)
	ListChatRooms(context.Context, string, int, int) (ChatlogRoomPage, error)
	ListSessions(context.Context, string, int, int) (ChatlogSessionPage, error)
}
```

The client must issue GET requests only, force `format=json`, use a loopback
base URL, reject cross-host redirects, and never fetch Chatlog media endpoints.

**Step 4: Write failing Worker runtime tests**

Test `build-info`, `check-config`, `doctor`, `once`, and `run`. Assert the Worker:

- reports `worker_type=chatlog-worker` and the approved protocol version;
- sends Source Agent heartbeat capability health for `chatlog_read`;
- services only Research Worker jobs targeted to its Agent ID;
- performs context expansion without keyword/sender filters;
- reports `dependency_unavailable` when the loopback API is down;
- never prints source content, aliases, token material, or local paths; and
- remains read-only.

**Step 5: Implement the Worker runtime**

Use `internal/sourceagentsecret` to load the same transport token service used
by the existing Workers. Combine `SourceAgentClient` heartbeat/maintenance
commands with `ResearchWorkerClient` job polling. Add a bounded local result
cache only if needed for idempotent retry; store hashes and locators, not full
conversation archives.

**Step 6: Run focused tests**

Run:

```bash
go test ./backend/app -run 'TestChatlogHTTP'
go test ./cmd/chatlog-agent
go test -race ./cmd/chatlog-agent
```

Expected: PASS.

**Step 7: Commit**

```bash
git add backend/app/chatlog_http.go backend/app/chatlog_http_test.go cmd/chatlog-agent/main.go cmd/chatlog-agent/main_test.go
git commit -m "feat(worker): add local chatlog research worker"
```

### Task 6: Package, install, diagnose, and remotely upgrade the Worker

**Files:**
- Create: `scripts/build-chatlog-agent-macos.sh`
- Create: `scripts/install-chatlog-agent-macos.sh`
- Create: `scripts/uninstall-chatlog-agent-macos.sh`
- Create: `scripts/chatlog-agent-packaging-smoke.sh`
- Modify: `backend/app/source_agent_update.go`
- Modify: `backend/app/source_agent_update_test.go`
- Modify: `backend/app/source_agent_update_bridge.go`
- Modify: `backend/app/source_agent_update_bridge_darwin_test.go`
- Modify: `backend/app/source_agent_update_activate_darwin.go`
- Modify: `backend/app/source_agent_update_activate_darwin_test.go`
- Modify: `cmd/source-agent-updater/main.go`
- Modify: `cmd/source-agent-updater/main_test.go`

**Step 1: Write the failing packaging smoke**

Model it after `scripts/wcplus-agent-packaging-smoke.sh`. Require:

- native arm64 and amd64 builds with matching `build-info`;
- exact revision binding;
- launchd label and updater label dedicated to `chatlog-worker`;
- existing Keychain transport-token service reuse;
- no token in plist, arguments, logs, or environment persistence;
- loopback Chatlog URL only;
- native-architecture verification before install and activation;
- `doctor` proving both local Chatlog API and remote auth; and
- recoverable install/uninstall and restricted update rollback.

**Step 2: Verify RED**

Run: `bash scripts/chatlog-agent-packaging-smoke.sh`

Expected: FAIL because the scripts and updater mapping do not exist.

**Step 3: Add `chatlog-worker` to the restricted updater catalog**

Extend the allowlists and mappings only:

```text
worker type: chatlog-worker
worker basename: chatlog-agent
worker label: life.executor.kbase.chatlog-agent
updater label: life.executor.kbase.chatlog-agent.updater
```

Do not generalize these mappings to arbitrary names. Preserve artifact hash,
version, platform, architecture, protocol, revision, ready-challenge, and
rollback checks.

**Step 4: Implement build/install/uninstall scripts**

Reuse `scripts/lib/managed-worker-install.sh` and
`scripts/lib/managed-worker-uninstall.sh`. Read the shared token from standard
input, save it through the existing Keychain loader, render launchd files with
loopback Chatlog URL and state/log paths, and run `check-config` plus `doctor`
before declaring readiness.

**Step 5: Run updater and packaging tests**

Run:

```bash
go test ./backend/app -run 'TestSourceAgent.*WorkerType|TestSourceAgentUpdaterActivator'
go test ./cmd/source-agent-updater
bash scripts/chatlog-agent-packaging-smoke.sh
bash scripts/managed-worker-install-smoke.sh
bash scripts/managed-worker-uninstall-smoke.sh
```

Expected: PASS.

**Step 6: Commit**

```bash
git add scripts/build-chatlog-agent-macos.sh scripts/install-chatlog-agent-macos.sh scripts/uninstall-chatlog-agent-macos.sh scripts/chatlog-agent-packaging-smoke.sh backend/app/source_agent_update.go backend/app/source_agent_update_test.go backend/app/source_agent_update_bridge.go backend/app/source_agent_update_bridge_darwin_test.go backend/app/source_agent_update_activate_darwin.go backend/app/source_agent_update_activate_darwin_test.go cmd/source-agent-updater/main.go cmd/source-agent-updater/main_test.go
git commit -m "build(worker): package chatlog agent"
```

### Task 7: Add versioned knowledge and prior-run research tools

**Files:**
- Create: `backend/app/research_tools.go`
- Test: `backend/app/research_tools_test.go`
- Modify: `backend/app/agent_runtime.go`
- Test: `backend/app/agent_runtime_test.go`

**Step 1: Write failing knowledge-tool tests**

Cover `search_knowledge`, `fetch_knowledge_evidence`, and `search_prior_runs`.
Assert package/version/release hashes are pinned, citation requirements remain
enforced, changed releases fail closed, previous runs return only verified
conclusions, and private evidence from a prior run is not copied without an
explicit new locator verification.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestResearchKnowledgeTool|TestResearchPriorRunTool'`

Expected: FAIL because the research tool registry is absent.

**Step 3: Implement the typed tool registry**

```go
type ResearchTool interface {
	Name() string
	Execute(context.Context, ResearchToolRequest) (ResearchToolResult, error)
}

type ResearchToolRequest struct {
	RunID          string
	PackageID      string
	PackageVersion string
	Arguments      map[string]any
}
```

Reuse `searchAgentPackageNaturalLanguageEvidence`, pinned release validation,
citation resolution, and Research Store read models. Do not fork a second
knowledge index or citation system.

**Step 4: Add policy audit records**

For every tool call, persist tool name, argument fingerprint, package scope,
policy decision, outcome, result fingerprint, duration, and promoted evidence
IDs. Never persist raw arguments containing names or message text in the audit
record.

**Step 5: Run focused tests**

Run: `go test ./backend/app -run 'TestResearchKnowledgeTool|TestResearchPriorRunTool|TestAgentPackage.*Search'`

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/research_tools.go backend/app/research_tools_test.go backend/app/agent_runtime.go backend/app/agent_runtime_test.go
git commit -m "feat(agent): add research knowledge tools"
```

### Task 8: Build identity, timeline, fact, conflict, and case invariants

**Files:**
- Create: `backend/app/research_analysis.go`
- Test: `backend/app/research_analysis_test.go`
- Create: `backend/app/testdata/research-analysis-v1.synthetic.json`

**Step 1: Add a synthetic fixture**

Use anonymous people, rooms, and sources. Include:

- one person with multiple aliases and an exact account binding;
- another person with the same display name but no binding evidence;
- a numeric series `24, 25, 18`;
- a direct recommendation and a general discussion;
- two recommendations with different timing or amount;
- a historical case with no confirmed recovery date; and
- a current case with materially different age, timing, and symptoms.

Do not copy any real chat message, public-account text, user name, group name,
account ID, or date into the fixture.

**Step 2: Write failing deterministic invariant tests**

```go
func TestResearchNumericTrendDoesNotCallMixedSeriesMonotonic(t *testing.T) {
	trend := ClassifyResearchNumericTrend([]float64{24, 25, 18})
	if trend.Direction != ResearchTrendMixed || trend.NetDirection != ResearchTrendDown {
		t.Fatalf("trend=%#v", trend)
	}
}
```

Also assert that name similarity alone remains ambiguous, missing recovery is
reported as not found, direct advice is not inferred from a general message,
and case transfer always lists material differences.

**Step 3: Verify RED**

Run: `go test ./backend/app -run 'TestResearchIdentity|TestResearchTimeline|TestResearchNumeric|TestResearchConflict|TestResearchCase'`

Expected: FAIL because the analysis engine is missing.

**Step 4: Implement typed analyzers**

Add:

```go
func ResolveResearchIdentity(candidates []ResearchIdentityCandidate) ResearchIdentityDecision
func BuildResearchTimeline(evidence []ResearchEvidence, facts []ResearchFact) []ResearchTimelineEvent
func ClassifyResearchNumericTrend(values []float64) ResearchNumericTrend
func DetectResearchConflicts(claims []ResearchClaim) []ResearchConflict
func CompareResearchCases(left, right ResearchCase) ResearchCaseComparison
```

Identity resolution may use exact account ID, contact metadata, group
membership, conversation continuity, self-identification, and confirmed
bindings. It must fail closed when multiple candidates remain plausible.

**Step 5: Persist structured analysis records**

Store facts, interventions, measurements, timeline events, conflicts, case
differences, support evidence IDs, confidence, and review state. A derived
record must never become its own sole evidence.

**Step 6: Run focused tests**

Run: `go test ./backend/app -run 'TestResearchIdentity|TestResearchTimeline|TestResearchNumeric|TestResearchConflict|TestResearchCase'`

Expected: PASS.

**Step 7: Commit**

```bash
git add backend/app/research_analysis.go backend/app/research_analysis_test.go backend/app/testdata/research-analysis-v1.synthetic.json
git commit -m "feat(agent): structure research analysis"
```

### Task 9: Implement the model roles and durable orchestration loop

**Files:**
- Create: `backend/app/research_model.go`
- Test: `backend/app/research_model_test.go`
- Create: `backend/app/research_orchestrator.go`
- Test: `backend/app/research_orchestrator_test.go`
- Modify: `backend/app/book_chat.go`
- Test: `backend/app/book_chat_test.go`

**Step 1: Write failing structured-model tests**

Use a fake `BookKnowledgeLLMClientWithResult` and test role-specific output for
planner, extractor, synthesizer, and verifier. Reject markdown-wrapped JSON,
unknown fields, over-limit arrays, unsupported tool names, unreferenced
evidence IDs, and conclusion text without support IDs.

The model output contains a bounded `decision_summary`; it must not request or
store hidden reasoning.

**Step 2: Verify model tests are RED**

Run: `go test ./backend/app -run 'TestResearchModel'`

Expected: FAIL because the model adapter is missing.

**Step 3: Implement role-specific structured calls**

```go
type ResearchModelRole string

const (
	ResearchRolePlanner     ResearchModelRole = "planner"
	ResearchRoleExtractor   ResearchModelRole = "extractor"
	ResearchRoleSynthesizer ResearchModelRole = "synthesizer"
	ResearchRoleVerifier    ResearchModelRole = "verifier"
)

type ResearchStageModel interface {
	Run(context.Context, ResearchModelRole, BookTokenPlanConfig, []BookKnowledgeMessage, any) (ResearchModelUsage, error)
}
```

Reuse `TokenPlanChatClient.ChatWithResult`, model normalization, usage, cost,
timeout, and Qwen thinking policy. Add a strict JSON response helper rather than
changing existing book chat behavior.

**Step 4: Write failing orchestrator loop tests**

Cover:

- quick path completes with existing grounded package retrieval;
- deep path plans, enqueues Worker tools, waits without busy looping, resumes on
  completion, extracts, compares, synthesizes, and verifies;
- verifier gaps return to planning with a bounded iteration count;
- `worker_offline`, `identity_ambiguous`, `zero_hit`, `partial_evidence`,
  `budget_exhausted`, `citation_mismatch`, `source_changed`, and
  `model_timeout` and `invalid_model_output` become typed outcomes;
- cancellation and restart recovery; and
- duplicate enqueue/model responses remain idempotent.

**Step 5: Verify orchestrator tests are RED**

Run: `go test ./backend/app -run 'TestResearchOrchestrator'`

Expected: FAIL because the loop is missing.

**Step 6: Implement the state machine**

Implement one orchestrator with role-separated stages. Each invocation performs
one durable unit of work, commits its step/event, and returns. Waiting for a
Worker job is represented by the current retrieval stage plus a typed wait
reason, not an in-memory blocked goroutine.

The verifier must check:

- every material conclusion has accessible support;
- every citation resolves to the expected hash;
- the cited set is distinguished from the searched set;
- identity and case-transfer warnings are present where required; and
- unsupported conclusions are removed or the run becomes `insufficient`.

**Step 7: Add the restart-safe coordinator**

Follow the lease, heartbeat, queue, scan, backoff, and shutdown patterns in
`backend/app/evidence_audit_coordinator.go`. Recover expired run leases and
pending Worker results on startup. Do not keep correctness only in process
memory.

**Step 8: Run focused and race tests**

Run:

```bash
go test ./backend/app -run 'TestResearchModel|TestResearchOrchestrator'
go test -race ./backend/app -run 'TestResearchOrchestrator'
```

Expected: PASS.

**Step 9: Commit**

```bash
git add backend/app/research_model.go backend/app/research_model_test.go backend/app/research_orchestrator.go backend/app/research_orchestrator_test.go backend/app/book_chat.go backend/app/book_chat_test.go
git commit -m "feat(agent): orchestrate deep research"
```

### Task 10: Expose the user Research API and server lifecycle

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

**Step 1: Write failing user API tests**

Define:

```text
POST /api/research/runs
GET  /api/research/runs/{run_id}
GET  /api/research/runs/{run_id}/events?after={sequence}
POST /api/research/runs/{run_id}/cancel
POST /api/research/runs/{run_id}/identity-bindings/{binding_id}/confirm
```

Test browser-cookie CSRF behavior, Bearer compatibility, method restrictions,
body limits, idempotency, run ownership boundary, cursor pagination, cancellation,
identity confirmation, and public projection redaction.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestKBaseHTTP.*ResearchRun'`

Expected: FAIL because the routes are absent.

**Step 3: Implement the handlers**

Return 201 for a new run and 200 for an idempotent replay. Return `202` while a
run is active. Map typed failures to stable codes without echoing private
source content or model bodies.

**Step 4: Wire server startup and shutdown**

Open one `ResearchStore` under the configured KBase root, create the
orchestrator and coordinator with the existing TokenPlan client, recover
runnable runs, and shut down with a bounded context. If initialization fails,
fail server startup; do not disable research silently after exposing the UI.

**Step 5: Add configuration validation**

Add bounded environment controls for coordinator workers, queue size, polling,
lease duration, model-role defaults, quick/deep budgets, and feature enablement.
Do not add a Chatlog database path to the server. The Chatlog endpoint belongs
only to the local Worker.

**Step 6: Run HTTP and lifecycle tests**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTP.*ResearchRun'
go test ./cmd/kbase-server -run 'Test.*Research'
```

Expected: PASS.

**Step 7: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go cmd/kbase-server/main_test.go
git commit -m "feat(kbase): expose research runs"
```

### Task 11: Version the Research capability in Agent Packages

**Files:**
- Modify: `backend/app/agent_package.go`
- Modify: `backend/app/agent_package_test.go`
- Modify: `backend/app/agent_tool_policy.go`
- Modify: `backend/app/agent_tool_policy_test.go`
- Modify: `backend/app/agent_compiler.go`
- Modify: `backend/app/agent_compiler_test.go`
- Modify: `backend/app/agent_package_store.go`
- Modify: `backend/app/agent_package_store_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing package-policy tests**

Add an opt-in `agent-package.v4` Research policy. Test that v1/v2/v3 hashes and
validation remain unchanged, v4 requires an explicit policy, normal compilation
does not gain Chatlog access, and a research-enabled compilation receives only
the approved read-only tools.

**Step 2: Verify RED**

Run: `go test ./backend/app -run 'TestAgentPackage.*Research|TestAgentCompilation.*Research'`

Expected: FAIL because v4 and the policy do not exist.

**Step 3: Add the explicit versioned policy**

```go
type AgentPackageResearchPolicy struct {
	Modes              []string `json:"modes"`
	AllowedSources     []string `json:"allowed_sources"`
	AllowedTools       []string `json:"allowed_tools"`
	MaxIterations      int      `json:"max_iterations"`
	MaxEvidenceItems   int      `json:"max_evidence_items"`
	MaxQuotedChars     int      `json:"max_quoted_chars"`
	MaxCostUSD         float64  `json:"max_cost_usd"`
	RequireVerification bool    `json:"require_verification"`
}
```

Allow only:

```text
research/search_chatlog
research/expand_chat_context
research/resolve_chat_identity
research/list_identity_conversations
research/fetch_chat_message
research/search_knowledge
research/fetch_knowledge_evidence
research/search_prior_runs
```

No send, delete, edit, export-all, media, shell, filesystem, or arbitrary HTTP
tool may enter this policy.

**Step 4: Add explicit compiler opt-in**

Extend the compilation request with `research_enabled`, default false. Keep
`allReadOnlyAgentCompilationTools()` book-only. When true, emit v4, append the
Research tools, add `deep_research` to the UI manifest, and use the approved
budgets. Ensure the compilation ID and content hash include the opt-in.

**Step 5: Preserve storage and runtime compatibility**

Treat v4 as a runtime-described immutable artifact everywhere v2 currently
requires a descriptor. Add `AgentPackageKnownToolIDs()` for package validation;
keep `AgentReadOnlyToolIDs()` and `allReadOnlyAgentCompilationTools()` book-only
so ordinary packages do not inherit Research tools. Reject v4 publication until
Task 13 supplies and passes the trusted `research-agent-v1` evaluation path.

**Step 6: Run package tests**

Run:

```bash
go test ./backend/app -run 'TestAgentPackage.*Research|TestAgentCompilation.*Research|TestAgentPackageStore.*V4'
go test ./backend/app -run 'TestAgentPackage|TestAgentCompilation|TestAgentToolPolicy|TestKBaseHTTP.*AgentPackage'
```

Expected: PASS, including unchanged legacy fixtures.

**Step 7: Commit**

```bash
git add backend/app/agent_package.go backend/app/agent_package_test.go backend/app/agent_tool_policy.go backend/app/agent_tool_policy_test.go backend/app/agent_compiler.go backend/app/agent_compiler_test.go backend/app/agent_package_store.go backend/app/agent_package_store_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(agent): version deep research policy"
```

### Task 12: Build the Chinese Research workspace and Agent entry point

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`
- Create: `frontend-web/scripts/research-workspace-smoke.mjs`

**Step 1: Write the failing UI smoke**

Assert routes `/research` and `/research/runs/{run_id}`, Chinese labels, quick/
deep/auto controls, package/version scope, start/cancel actions, resumable event
polling, evidence/timeline/conflict/report tabs, identity confirmation, typed
failure actions, and an Agent-console “深度研究” entry visible only when the
package advertises `deep_research`.

Also assert there is no chain-of-thought label, raw Worker payload, private
identifier dump, token input, or full-chat export button.

**Step 2: Verify RED**

Run: `node frontend-web/scripts/research-workspace-smoke.mjs`

Expected: FAIL because the route and renderer are absent.

**Step 3: Add route, state, and API helpers**

Keep latest-request guards and abort controllers separate for list, run detail,
and event polling. Replace stale run content immediately when changing run IDs.
Use `after` sequence cursors and stop polling terminal runs.

**Step 4: Render the dense workspace**

Build:

- a compact question/mode/source header;
- a stage progress rail showing bounded decision summaries;
- searched scope versus cited scope;
- evidence cards with source role, time, selected excerpt, and locator status;
- identity ambiguity and confirmation panel;
- chronological timeline with measurements and interventions;
- conflict and case-difference tables; and
- final report with claim-level citation links and insufficiency notices.

Do not show model hidden reasoning or imply that searched sources equal cited
sources.

**Step 5: Add responsive and accessible styling**

Verify 1280×720 and 390×844 CSS-pixel layouts, no horizontal overflow, keyboard
navigation, visible focus, correct tabs/regions, dialog focus return, readable
Chinese typography, reduced motion, and status text that does not rely on color.

**Step 6: Run frontend checks**

Run:

```bash
node frontend-web/scripts/research-workspace-smoke.mjs
node frontend-web/scripts/agent-console-ui-smoke.mjs
cd frontend && npm run build
```

Expected: PASS. Preserve the real exit code; do not pipe through plain `tail`.

**Step 7: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html frontend-web/scripts/research-workspace-smoke.mjs
git commit -m "feat(web): add research workspace"
```

### Task 13: Add the gold evaluation suite and prove a real end-to-end run

**Files:**
- Create: `backend/app/research_eval.go`
- Test: `backend/app/research_eval_test.go`
- Create: `backend/app/testdata/research-evaluation-v1.synthetic.json`
- Create: `scripts/research-agent-smoke.sh`
- Modify: `backend/app/agent_package_evaluation.go`
- Modify: `backend/app/agent_package_evaluation_test.go`
- Modify: `backend/app/trusted_agent_evaluation.go`
- Modify: `backend/app/evidence_audit_runner.go`
- Modify: `backend/app/evidence_audit_runner_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `docs/dossiers/2026-08-13-research-agent-platform.md`

**Step 1: Write the synthetic gold suite**

Include quick and deep questions with expected retrieval scope, identity result,
timeline events, trend classification, conflict set, case differences,
conclusion support, citation coverage, and expected abstention/failure code.
Keep every person, source, message, ID, and date synthetic.

**Step 2: Write failing evaluator tests**

Score:

- identity precision and ambiguity handling;
- timeline event precision/recall;
- numeric trend correctness;
- direct-advice classification;
- intervention/conflict extraction;
- case-transfer warning coverage;
- material-claim citation coverage;
- safe insufficiency;
- private-data projection; and
- latency/cost budgets.

Hard-fail a candidate that fabricates recovery, labels `24,25,18` monotonic,
uses an ambiguous identity, transfers an amount without case differences, or
publishes an unsupported conclusion.

**Step 3: Verify RED**

Run: `go test ./backend/app -run 'TestResearchEvaluation'`

Expected: FAIL because the evaluator is missing.

**Step 4: Implement deterministic scoring**

Save the suite version and input hash with the evaluation report. Separate
deterministic hard gates from model-graded language quality. Do not let a high
aggregate score override a failed privacy, identity, citation, or fabrication
gate.

Wire v4 evaluation, persistence, HTTP evaluate/publish, and publish-time
recomputation to the trusted Research suite. Permit an evidence-capable v4
package in the existing evidence-audit runner only when `evidence_policy` is
present, and require the trusted Research suite to include both Research and
evidence hard gates for that combination. Keep v2 behavior byte-for-byte
compatible.

**Step 5: Add the full-stack smoke**

`scripts/research-agent-smoke.sh` must start an isolated KBase server, a fake
Chatlog loopback service, and `chatlog-agent once`; create a deep run; wait with
a bounded timeout; and assert a verified report, citations, events, Worker
heartbeats, and no raw sentinel leakage. It must also test Worker-offline and
identity-ambiguous outcomes.

**Step 6: Run the synthetic suite and smoke**

Run:

```bash
go test ./backend/app -run 'TestResearchEvaluation'
bash scripts/research-agent-smoke.sh
```

Expected: PASS.

**Step 7: Run one authorized real-data acceptance locally**

Against the real loopback Chatlog service and a selected published collection
package:

1. run one quick grounded question;
2. run one deep historical/current-case comparison;
3. inspect identity evidence, context expansion, timeline, conflicts, case
   differences, citations, and searched scope;
4. re-fetch at least one cited Chatlog locator and one knowledge citation;
5. restart the coordinator during a second deep run and verify resume; and
6. confirm the stored Research database does not contain the full raw responses.

Record only run IDs, content hashes, aggregate counts, timings, outcome codes,
and gate verdicts in the dossier. Do not commit real questions, names, messages,
locators, screenshots containing private data, or exported Chatlog content.

**Step 8: Update Gate evidence and commit**

Mark only gates actually supported by the test output. Then commit:

```bash
git add backend/app/research_eval.go backend/app/research_eval_test.go backend/app/testdata/research-evaluation-v1.synthetic.json scripts/research-agent-smoke.sh backend/app/agent_package_evaluation.go backend/app/agent_package_evaluation_test.go backend/app/trusted_agent_evaluation.go backend/app/evidence_audit_runner.go backend/app/evidence_audit_runner_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go docs/dossiers/2026-08-13-research-agent-platform.md
git commit -m "test(agent): qualify research runtime"
```

### Task 14: Review, generate the system map, merge, deploy, and verify online

**Files:**
- Modify: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-08-13-research-agent-platform.md`
- Modify: `README.md`
- Modify: `.github/workflows/kbase-build-gates.yml`

**Step 1: Regenerate architecture truth**

Run:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

Expected: PASS. Never hand-edit architecture counts.

**Step 2: Add CI and operator documentation**

Document the Research API, Worker dependency, shared-token setup, local
loopback requirement, install/doctor/update/rollback commands, privacy boundary,
quick/deep behavior, and failure recovery. Add focused tests, packaging smoke,
and research smoke to CI without embedding credentials or local paths.

**Step 3: Run the complete G3 suite**

Run without plain `tail`:

```bash
go mod verify
go vet ./...
go test ./... -timeout=600s -count=1
cd frontend && npm run build
cd ..
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
bash scripts/research-agent-smoke.sh
bash scripts/chatlog-agent-packaging-smoke.sh
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: every command exits 0. Any failure returns to its owning task.

**Step 4: Obtain G4 review**

Use `@requesting-code-review` for specification, correctness, privacy, auth,
lease/idempotency, model-grounding, packaging/rollback, and UI review. Resolve
all Critical and Important findings and rerun affected tests. Record every
NO-GO and later remediation in the dossier; do not erase failed review history.

**Step 5: Commit final docs and generated map**

```bash
git add docs/_generated/system-map.json docs/dossiers/2026-08-13-research-agent-platform.md README.md .github/workflows/kbase-build-gates.yml
git commit -m "docs(agent): record research release gates"
```

**Step 6: Merge to a clean `main`**

Use `@finishing-a-development-branch`. Confirm the integration target contains
no unrelated dirty files, merge the reviewed branch, rerun privacy and diff
checks, and push only after the merge commit succeeds.

**Step 7: Publish the first research-enabled Agent version**

Select the intended immutable public-article collection release, compile with
`research_enabled=true`, run `research-agent-v1`, and publish a new semantic
version through the existing controlled Agent workflow. Do not mutate an
already-published Agent Package.

**Step 8: Deploy KBase with the direct-deployment contract**

Build from the exact clean `main` revision, run the production build gates,
back up the scoped KBase state, cut over with
`scripts/kbase-direct-deployment-cutover.sh`, and verify loopback plus public
`/health` return the exact revision. Do not add a release signature mechanism.

**Step 9: Install and verify the macOS Chatlog Worker**

Build the native Worker and shared updater, install with the existing Worker
token supplied on standard input, verify launchd state, `doctor`, heartbeat,
capability health, exact revision, updater readiness, and rollback metadata.
Do not expose the local Chatlog port beyond loopback.

**Step 10: Perform G5/G6 production verification**

Through the authenticated online service and the real local Worker:

- anonymous Research and Worker APIs return 401;
- browser session and CSRF behavior are correct;
- shared Worker token claims and completes only its own jobs;
- quick mode completes with grounded citations and meets the 15-second target;
- deep mode searches knowledge and Chatlog, expands each key context, and
  normally completes within the three-minute target;
- a Worker outage produces `worker_offline`, never an ungrounded answer;
- identity ambiguity requests operator confirmation;
- citations re-fetch to the recorded hashes;
- `/research` and the Agent entry point render correctly in Chinese on desktop
  and mobile widths; and
- browser console, service logs, and Worker logs contain no new errors,
  warnings, tokens, source bodies, or private paths.

**Step 11: Close the dossier only with evidence**

Record the deployed revision, Agent Package version/hash, Worker version/hash,
test commands, service health, aggregate run metrics, rollback availability,
and G1–G6 verdicts. If any gate is red or blocked, leave the feature open and
return to the failed upstream task.
