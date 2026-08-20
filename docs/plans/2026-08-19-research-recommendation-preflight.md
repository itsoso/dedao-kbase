# Research Agent Recommendation Preflight Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let a user enter a research question, receive three explainable eligible Agent recommendations, confirm one after deterministic readiness checks, and create an immutable linked Research Run without knowing Package IDs.

**Architecture:** Add a model-free recommendation service over published v4 Research packages, persist owner-scoped short-lived preflight snapshots in the existing Research SQLite store, and require the selected snapshot when creating a Run. The Web workspace debounces preflight requests, renders compact candidate/readiness cards, and creates linked retries while the server revalidates package policy, content hash, Worker state, and budget.

**Tech Stack:** Go 1.23, SQLite, existing BookKnowledgeStore, ResearchStore and SourceSyncStore, net/http, vanilla JavaScript/CSS in frontend-web, Node smoke scripts, shell process smoke.

---

## Delivery rules

- Work only in the dedicated feature worktree and preserve unrelated changes.
- Use TDD for every behavior change: red test, minimal implementation, green test, focused commit.
- Reuse loadRunnableResearchAgentPackageScope, ValidateAgentPackageEvaluationGate, DeriveSourceAgentObservedState, RouteResearchMode, and current Research budgets. Do not create a second policy engine.
- Do not add a signature or new authentication mechanism. Use the existing shared Token/browser owner scope.
- Do not log or return question bodies, evidence bodies, chat identities, local paths, locators, cookies, or tokens.
- Regenerate the system map after adding the route and run all G3/G4 gates before merge.

### Task 1: Open the feature dossier and define the preflight domain contract

**Files:**
- Create: docs/dossiers/2026-08-19-research-recommendation-preflight.md
- Create: backend/app/research_preflight.go
- Create: backend/app/research_preflight_test.go
- Reference: docs/plans/2026-08-18-research-recommendation-preflight-design.md
- Reference: backend/app/research_run.go:58-172

**Step 1: Create the dossier with G1 PASS and G2-G6 pending**

Record the approved human-confirmation boundary, deterministic recommendation choice, no-global-fallback rule, shared Token constraint, privacy boundary, and a generic account-collection acceptance case. Do not copy a real medical question or private identity into the dossier.

**Step 2: Write failing contract tests**

Add tests for normalized requests, hard bounds, stable source ordering, candidate limit, and safe public projection:

~~~go
func TestValidateResearchPreflightRequestNormalizesBoundedScope(t *testing.T) {
    request, err := NormalizeResearchPreflightRequest(ResearchPreflightRequest{
        Mode: " auto ", Question: "  compare evidence  ",
        RequestedSources: []string{"knowledge", "chatlog"},
    })
    if err != nil { t.Fatal(err) }
    if request.Question != "compare evidence" || request.Mode != ResearchModeAuto {
        t.Fatalf("normalized request = %#v", request)
    }
}

func TestResearchPreflightProjectionContainsNoPrivateBodies(t *testing.T) {
    encoded, err := json.Marshal(PublicResearchPreflight(testResearchPreflight()))
    if err != nil { t.Fatal(err) }
    for _, forbidden := range []string{"content_excerpt", "message_ref", "local_path", "identity_id"} {
        if bytes.Contains(encoded, []byte(forbidden)) { t.Fatalf("projection leaks %s", forbidden) }
    }
}
~~~

**Step 3: Run the tests and verify RED**

Run:

~~~bash
go test ./backend/app -run 'TestValidateResearchPreflight|TestResearchPreflightProjection' -count=1
~~~

Expected: FAIL because the preflight types and functions do not exist.

**Step 4: Implement the minimal domain types and validators**

Define bounded enums and structures:

~~~go
type ResearchPreflightRequest struct {
    Mode string `json:"mode"`
    Question string `json:"question"`
    RequestedSources []string `json:"requested_sources,omitempty"`
    PackageConstraint string `json:"package_constraint,omitempty"`
    ParentRunID string `json:"parent_run_id,omitempty"`
}

type ResearchPreflight struct {
    PreflightID string `json:"preflight_id"`
    RequestHash string `json:"-"`
    Status string `json:"status"`
    Candidates []ResearchPreflightCandidate `json:"candidates"`
    Checks []ResearchPreflightCheck `json:"checks"`
    Gaps []ResearchPreflightGap `json:"gaps,omitempty"`
    ParentRunID string `json:"parent_run_id,omitempty"`
    CreatedAt string `json:"created_at"`
    ExpiresAt string `json:"expires_at"`
}
~~~

Use high|medium|low for match level and pass|warning|blocked for checks. Reuse existing Research question/source bounds. PublicResearchPreflight must build an explicit safe projection.

**Step 5: Run focused tests and commit**

~~~bash
go test ./backend/app -run 'TestValidateResearchPreflight|TestResearchPreflightProjection' -count=1
git add docs/dossiers/2026-08-19-research-recommendation-preflight.md backend/app/research_preflight.go backend/app/research_preflight_test.go
git commit -m "feat(research): define recommendation preflight contract"
~~~

### Task 2: Implement deterministic eligibility and ranking

**Files:**
- Modify: backend/app/research_preflight.go
- Modify: backend/app/research_preflight_test.go
- Reference: backend/app/agent_runtime.go:771-867
- Reference: backend/app/agent_package_evaluation.go:943-980
- Reference: backend/app/agent_package.go:44-160

**Step 1: Write failing table tests**

Cover:

- only published v4 packages with Research Policy and trusted passing evaluation are eligible;
- mode and requested sources must be policy subsets;
- packages without search and deep_research are rejected;
- evidence coverage outranks metadata-only matches;
- readiness affects warnings/blocks, not policy eligibility;
- stable tie-break is normalized Package ID, version, then content hash;
- at most three candidates return;
- an explicit Package constraint never widens eligibility.

Use reason codes topic_match, evidence_coverage, fresh_release, trusted_evaluation, and worker_ready. Do not persist free-form model prose.

**Step 2: Verify RED**

~~~bash
go test ./backend/app -run 'TestRankResearchPreflight|TestResearchPreflightEligibility' -count=1
~~~

Expected: FAIL because the ranker is missing.

**Step 3: Implement a pure ranker**

Keep I/O outside the ranker:

~~~go
type ResearchPreflightPackageFacts struct {
    Package AgentPackage
    TopicHits int
    EvidenceHits int
    LatestPublishedAt string
    EvaluationPassed bool
    WorkerState string
    BudgetFits bool
}

func RankResearchPreflightCandidates(
    request ResearchPreflightRequest,
    facts []ResearchPreflightPackageFacts,
) ([]ResearchPreflightCandidate, []ResearchPreflightGap)
~~~

Call existing package validation and scope helpers for the hard gate. Convert signals to ordinal buckets; never expose a percentage. Sort with a complete deterministic tie-break and copy only safe summary fields.

**Step 4: Run focused and race tests**

~~~bash
go test ./backend/app -run 'TestRankResearchPreflight|TestResearchPreflightEligibility' -count=1
go test -race ./backend/app -run 'TestRankResearchPreflight|TestResearchPreflightEligibility' -count=1
~~~

Expected: PASS.

**Step 5: Commit**

~~~bash
git add backend/app/research_preflight.go backend/app/research_preflight_test.go
git commit -m "feat(research): rank eligible Agent recommendations"
~~~

### Task 3: Persist owner-scoped expiring preflight snapshots

**Files:**
- Modify: backend/app/research_store.go:88-240
- Create: backend/app/research_preflight_store.go
- Create: backend/app/research_preflight_store_test.go

**Step 1: Write failing migration and store tests**

Test create/load, owner isolation, request hash conflict, ten-minute expiry, immutable replay, no raw question column, bounded JSON, cleanup, and restart persistence. Inspect PRAGMA table_info(research_preflights) to prove there is no question, content, message, locator, or path column.

**Step 2: Verify RED**

~~~bash
go test ./backend/app -run 'TestResearchPreflightStore' -count=1
~~~

Expected: FAIL because the table and store methods do not exist.

**Step 3: Add the migration**

~~~sql
CREATE TABLE IF NOT EXISTS research_preflights (
    preflight_id TEXT PRIMARY KEY,
    owner_hash TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    candidates_json TEXT NOT NULL,
    checks_json TEXT NOT NULL,
    gaps_json TEXT NOT NULL,
    parent_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_research_preflights_owner_expiry
    ON research_preflights(owner_hash, expires_at, preflight_id);
~~~

Do not add a raw question column. The request hash covers normalized question, mode, sorted sources, constraint, parent Run, and schema version.

**Step 4: Implement store methods**

~~~go
func (s *ResearchStore) SaveResearchPreflight(ownerHash string, request ResearchPreflightRequest, result ResearchPreflight, ttl time.Duration) (*ResearchPreflight, error)
func (s *ResearchStore) LoadResearchPreflightForOwner(preflightID, ownerHash string) (*ResearchPreflight, error)
func (s *ResearchStore) DeleteExpiredResearchPreflights(limit int) (int, error)
~~~

Return typed not-found, expired, owner, and idempotency errors. Decode JSON with strict bounds and fail closed on corrupted persisted data.

**Step 5: Run tests and commit**

~~~bash
go test ./backend/app -run 'TestResearchPreflightStore|TestOpenResearchStore' -count=1
git add backend/app/research_store.go backend/app/research_preflight_store.go backend/app/research_preflight_store_test.go
git commit -m "feat(research): persist recommendation preflights"
~~~

### Task 4: Resolve package coverage, Worker readiness, and budget

**Files:**
- Create: backend/app/research_preflight_service.go
- Create: backend/app/research_preflight_service_test.go
- Modify: backend/app/research_preflight.go
- Reference: backend/app/agent_package_store.go:192-360
- Reference: backend/app/agent_runtime.go:68-130
- Reference: backend/app/source_agent_control.go:38-80
- Reference: backend/app/source_sync.go:70-135

**Step 1: Write failing service tests**

Create multiple published packages and prove:

- the package with evidence hits for the question ranks first;
- the response contains counts and Release metadata, never hit text;
- knowledge-only reports Worker not_required;
- chatlog blocks when chatlog-agent is stale or unhealthy;
- a fresh authenticated heartbeat passes readiness;
- budget uses resolved quick/deep mode and the smaller Package/server limits;
- Package/evaluation corruption is a typed block while storage errors remain retryable;
- no eligible package returns no_eligible_package and an Agent completion action.

**Step 2: Verify RED**

~~~bash
go test ./backend/app -run 'TestResearchPreflightService' -count=1
~~~

Expected: FAIL because the service is missing.

**Step 3: Implement the service**

~~~go
type ResearchPreflightService struct {
    Knowledge *BookKnowledgeStore
    Research *ResearchStore
    SourceSync *SourceSyncStore
    QuickBudget ResearchBudget
    DeepBudget ResearchBudget
    Now func() time.Time
}

func (s *ResearchPreflightService) Evaluate(
    ctx context.Context,
    ownerHash string,
    request ResearchPreflightRequest,
) (*ResearchPreflight, error)
~~~

Page through ListAgentPackages, load exact published packages, reuse loadRunnableResearchAgentPackageScope, and run a bounded searchAgentPackageEvidence probe. Immediately discard evidence bodies after counting hits and distinct Release/citation coverage.

Use SourceSyncStore.GetSourceAgent("chatlog-agent") plus DeriveSourceAgentObservedState; do not infer online state from queued jobs. Budget calculation uses RouteResearchMode, configured server budget, and Package Research limits. Do not call a model.

**Step 4: Run focused and race tests**

~~~bash
go test ./backend/app -run 'TestResearchPreflightService' -count=1
go test -race ./backend/app -run 'TestResearchPreflightService' -count=1
~~~

**Step 5: Commit**

~~~bash
git add backend/app/research_preflight.go backend/app/research_preflight_service.go backend/app/research_preflight_service_test.go
git commit -m "feat(research): evaluate package and runtime readiness"
~~~

### Task 5: Expose the authenticated preflight HTTP endpoint

**Files:**
- Modify: backend/app/kbase_http.go:25-75, 115-285, 415-430, 4668-4820
- Modify: backend/app/kbase_http_test.go:2513-2820
- Modify: cmd/kbase-server/main.go:305-330
- Modify: docs/_generated/system-map.json

**Step 1: Write failing HTTP tests**

Add cases for bearer and browser-session auth, CSRF, owner isolation, unknown fields, body limit, bad mode/source, ready response, blocked response, redaction, method rejection, and dependency unavailable behavior.

~~~go
response := requestJSONKBase(handler, http.MethodPost, "/api/research/preflight", "admin-secret", `{
  "mode":"auto","question":"compare evidence","requested_sources":["knowledge"]
}`)
if response.Code != http.StatusCreated { t.Fatalf("status = %d", response.Code) }
~~~

**Step 2: Verify RED**

~~~bash
go test ./backend/app -run 'TestKBaseHTTPResearchPreflight' -count=1
~~~

Expected: FAIL with a missing route.

**Step 3: Wire the service and route**

Construct the service from the already supplied Store, ResearchStore, SourceSync, and budgets. Route exact path /api/research/preflight before the /api/research/runs prefix. Reuse strict JSON decoding, owner hashing, CSRF, and current error helpers. Map typed errors to stable public codes without exposing filesystem, artifact, or database details.

**Step 4: Regenerate the map and run tests**

~~~bash
go test ./backend/app ./cmd/kbase-server -run 'TestKBaseHTTPResearchPreflight|TestKBaseHTTPResearchRun' -count=1
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
~~~

**Step 5: Commit**

~~~bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go docs/_generated/system-map.json
git commit -m "feat(research): expose Agent recommendation preflight"
~~~

### Task 6: Require a fresh selected preflight when creating a Run

**Files:**
- Modify: backend/app/research_run.go:58-126
- Modify: backend/app/research_run_test.go
- Modify: backend/app/research_store.go
- Modify: backend/app/research_preflight_store.go
- Modify: backend/app/research_preflight_store_test.go
- Modify: backend/app/kbase_http.go:4755-4820
- Modify: backend/app/kbase_http_test.go:2513-2820
- Modify: scripts/research-agent-smoke.sh:180-250

**Step 1: Write failing confirmation tests**

Cover missing preflight, expired preflight, wrong owner, changed question/mode/sources, candidate outside snapshot, changed Package hash, readiness drift, idempotent replay, concurrent confirmation, and parent Run ownership. Assert no research_runs row exists for every rejection.

**Step 2: Verify RED**

~~~bash
go test ./backend/app -run 'TestResearchRunRequiresPreflight|TestKBaseHTTPResearchRun.*Preflight' -count=1
~~~

**Step 3: Extend request and Run linkage**

Add PreflightID to ResearchRunRequest. Add ParentRunID and PreflightID to ResearchRun and backward-compatible columns to research_runs. Validate with current bounded identifier rules.

**Step 4: Confirm and insert atomically in Research Store**

Before the transaction, reload the exact Package and reevaluate policy, evaluation, content hash, Worker, and budget. In one Research Store transaction, load the owner preflight, verify request hash and selected candidate, check expiry, and insert/replay the Run. Bind a successful confirmation to the resulting Run ID without breaking safe HTTP replay.

**Step 5: Update the process smoke**

For quick knowledge and deep Chatlog:

1. POST /api/research/preflight.
2. Assert the seeded Package is eligible.
3. POST /api/research/runs with preflight_id and selected identity.
4. Preserve all completion and citation assertions.

**Step 6: Run and commit**

~~~bash
go test ./backend/app ./cmd/kbase-server -run 'TestResearchRunRequiresPreflight|TestKBaseHTTPResearchRun|TestResearchPreflightStore' -count=1
bash scripts/research-agent-smoke.sh
git add backend/app/research_run.go backend/app/research_run_test.go backend/app/research_store.go backend/app/research_preflight_store.go backend/app/research_preflight_store_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go scripts/research-agent-smoke.sh
git commit -m "feat(research): bind Run creation to preflight"
~~~

### Task 7: Build the compact Web recommendation and readiness UI

**Files:**
- Modify: frontend-web/app.js:355-395, 11480-11640
- Modify: frontend-web/styles.css near the Research dossier styles
- Modify: frontend-web/scripts/research-workspace-smoke.mjs
- Modify: frontend-web/index.html

**Step 1: Extend the smoke with failing assertions**

Require:

- an independent researchPreflightRequestController and debounce timer;
- POST /api/research/preflight;
- stale response rejection;
- at most three candidate cards and default selection;
- match reasons, knowledge range, evaluation, Worker, source, and budget checks;
- blocked start button and Agent completion link;
- no raw Package ID text inputs in the normal flow;
- Chinese mappings for every preflight error;
- keyboard radio/card semantics and aria-live readiness;
- a fresh cache marker.

**Step 2: Verify RED**

~~~bash
node frontend-web/scripts/research-workspace-smoke.mjs
~~~

**Step 3: Extend state and request control**

Add preflight, selectedCandidateKey, loading.preflight, parentRunID, a latest-request controller, and a 600ms debounce. Normalize the draft before starting. Cancel on route change. Ignore responses whose sequence or draft fingerprint is stale.

**Step 4: Render three compact zones**

Replace Package ID/version text fields with candidate cards. Render high/medium/low as 高匹配/中匹配/低匹配. Default to the first candidate only for a new preflight; preserve a still-valid manual selection.

Render checks with pass/warning/blocked. Disable 开始研究 when preflight is missing, loading, expired, blocked, or no candidate is selected. Submit exact preflight_id, Package ID, and version.

**Step 5: Add responsive and accessible styles**

Use real radio controls or an equivalent keyboard-operable pattern. At 760px collapse cards/checks to one column. Keep opaque IDs in expandable details and wrap anywhere.

**Step 6: Run and commit**

~~~bash
node --check frontend-web/app.js
node frontend-web/scripts/research-workspace-smoke.mjs
node frontend-web/scripts/browser-cookie-session-smoke.mjs
node frontend-web/scripts/agent-console-ui-smoke.mjs
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/research-workspace-smoke.mjs frontend-web/index.html
git commit -m "feat(web): recommend Research Agents before launch"
~~~

### Task 8: Add immutable linked retry

**Files:**
- Modify: frontend-web/app.js:11535-11640
- Modify: frontend-web/styles.css
- Modify: frontend-web/scripts/research-workspace-smoke.mjs
- Modify: backend/app/kbase_http_test.go

**Step 1: Write failing assertions**

Prove failed/insufficient detail offers 调整并重试; the action returns to /research with inherited state held in memory, not sensitive query parameters; parent_run_id is sent only after owner-visible detail loads; suggestions map fixed outcomes to bounded actions; and the parent remains unchanged.

**Step 2: Verify RED**

~~~bash
node frontend-web/scripts/research-workspace-smoke.mjs
go test ./backend/app -run 'TestKBaseHTTPResearchRun.*Parent' -count=1
~~~

**Step 3: Implement linked retry**

Copy only question, mode, sources, and selected Package identity into a fresh draft, set parentRunID, clear old preflight, navigate to the new workspace, and schedule preflight. Show suggestions from the public failure code only.

**Step 4: Run and commit**

~~~bash
node --check frontend-web/app.js
node frontend-web/scripts/research-workspace-smoke.mjs
go test ./backend/app -run 'TestKBaseHTTPResearchRun.*Parent' -count=1
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/research-workspace-smoke.mjs backend/app/kbase_http_test.go
git commit -m "feat(research): create immutable linked retries"
~~~

### Task 9: Complete process, privacy, performance, and review gates

**Files:**
- Modify: scripts/research-agent-smoke.sh
- Modify: docs/dossiers/2026-08-19-research-recommendation-preflight.md
- Modify if generated: docs/_generated/system-map.json

**Step 1: Expand the process smoke**

Assert the intended synthetic Package is first and no more than three candidates return; knowledge-only says Worker not required; Chatlog blocks before heartbeat and becomes ready after heartbeat; exact confirmation completes quick/deep Runs; expired or wrong-owner creates no Run; linked retry records its parent; citation re-fetch still works. Do not print seeded bodies.

**Step 2: Add a bounded performance test**

Seed a realistic bounded catalog and test the pure ranker and fixture-backed service under a generous deterministic unit ceiling. Production P95 is measured in G6, not asserted with a flaky unit timer.

**Step 3: Run focused race and process gates**

~~~bash
go test -race ./backend/app ./cmd/kbase-server -count=1 -run 'TestResearchPreflight|TestResearchRunRequiresPreflight|TestKBaseHTTPResearch'
bash scripts/research-agent-smoke.sh
node --check frontend-web/app.js
node frontend-web/scripts/research-workspace-smoke.mjs
~~~

**Step 4: Run complete G3**

Generate frontend/dist first because the Wails root embeds it:

~~~bash
cd frontend
npm ci
npm run build
cd ..
go mod verify
go vet ./...
go test ./...
bash scripts/privacy-smoke.sh
bash scripts/system-map-smoke.sh
git diff --check
git status --short
~~~

**Step 5: Request independent G4 review**

Review fail-closed eligibility, owner isolation, expiry/replay, cross-store races, Worker freshness, budget estimation, raw-data redaction, stale browser responses, accessibility, migration rollback, and smoke validity. Fix every Critical/Important finding and rerun G3.

**Step 6: Record G3/G4 and commit**

~~~bash
git add docs/dossiers/2026-08-19-research-recommendation-preflight.md docs/_generated/system-map.json
git commit -m "docs(research): record recommendation preflight gates"
~~~

### Task 10: Merge, deploy, and run real production acceptance

**Files:**
- Modify after acceptance: docs/dossiers/2026-08-19-research-recommendation-preflight.md
- Production targets: existing KBase binary and frontend-web directory only

**Step 1: Merge a clean reviewed candidate**

Fetch canonical dedao-kbase/main, require it is an ancestor, merge in a clean release worktree, rerun privacy/system-map/diff checks, push the exact candidate, and verify remote equality. Do not touch the dirty primary checkout.

**Step 2: Build and verify on Linux**

Archive the exact revision, verify SHA-256 after upload, build as the service account, and run frontend build/smokes, go mod verify, vet, full tests, --check-config, and an isolated exact-revision health probe. Use the approved checksum-valid alternate Go proxy only if the default proxy times out.

**Step 3: Back up and cut over atomically**

Under the deployment lock, back up the installed KBase binary and Web directory. Stage same-filesystem candidates, verify hashes, restart, and require loopback health with the exact revision. On failure restore both backups and stop G5.

**Step 4: Perform authenticated real acceptance**

Using an approved account-collection Research Agent and a generic synthetic question:

1. Verify the intended Agent is first with bounded reasons.
2. Verify knowledge-only does not depend on Chatlog.
3. Enable Chatlog and verify actual Worker readiness.
4. Use a controlled fixture boundary to prove offline blocking without disrupting unrelated work.
5. Restore readiness and create a Run.
6. Verify no empty/invalid Run, final citations, and linked retry from an insufficient fixture.
7. Measure preflight latency and require production P95 under 1.5 seconds for the bounded sample.

Do not record the real question, evidence, chat content, identities, cookies, or tokens.

**Step 5: Verify G5/G6 and record rollback evidence**

Require public/loopback health, active service, exit zero, no restarts, Web routes 200, anonymous API 401, Chinese browser controls, no panic/fatal logs, binary hash, and backup path.

~~~bash
bash scripts/privacy-smoke.sh
git diff --check
git add docs/dossiers/2026-08-19-research-recommendation-preflight.md
git commit -m "docs(research): record recommendation preflight rollout"
git push dedao-kbase HEAD:main
~~~

Expected: G5/G6 PASS and canonical main contains only privacy-safe rollout evidence.
