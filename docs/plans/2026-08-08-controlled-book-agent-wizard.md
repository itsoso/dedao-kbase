# Controlled Book Agent Wizard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Repair deterministic ebook content identity, expose safe initial knowledge release publication, and add a human-controlled Book Agent authoring, evaluation, and publication wizard.

**Architecture:** The knowledge store computes one canonical hash over durable retrieval objects and repairs only legacy empty identities. The Web UI advances through existing analysis and release contracts, while a narrow server-side Agent workflow creates a contract-valid draft and delegates evaluation/publication to the existing publisher-gated implementation without returning its credential to the browser.

**Tech Stack:** Go, JSON contracts, net/http, Vue-independent vanilla Web UI in `frontend-web`, Node smoke tests, systemd deployment.

---

### Task 1: Canonical Content Hash

**Files:**
- Modify: `backend/app/book_knowledge.go`
- Modify: `backend/app/book_knowledge_store.go`
- Test: `backend/app/book_knowledge_test.go`
- Test: `backend/app/book_knowledge_store_test.go`

**Step 1: Write the failing test**

Add tests proving that canonical hashing is stable across repeated calls, changes
when a durable retrieval object changes, and ignores volatile timestamps.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'TestBookKnowledgeContentHash' -count=1`

Expected: FAIL because the canonical hash helper does not exist.

**Step 3: Write minimal implementation**

Add a canonical projection and SHA-256 helper. Normalize unordered collections
before JSON encoding and return a `sha256:` prefixed identity.

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'TestBookKnowledgeContentHash' -count=1`

Expected: PASS.

**Step 5: Commit**

Stage only the four Task 1 files and commit `fix(kbase): generate stable book content hashes`.

### Task 2: Dedao Import And Legacy Repair

**Files:**
- Modify: `backend/app/dedao_ebook_jobs.go`
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/dedao_ebook_jobs_test.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write the failing tests**

Add one import test proving a synchronized ebook is saved with a non-empty
canonical hash. Add HTTP tests proving repair succeeds only for an empty hash,
invalidates its stale analysis/quality artifacts, and refuses to rewrite a
non-empty hash.

**Step 2: Run tests to verify they fail**

Run: `go test ./backend/app -run 'Test.*(DedaoEbook.*ContentHash|RepairBookContentHash)' -count=1`

Expected: FAIL for missing import assignment and repair route.

**Step 3: Write minimal implementation**

Assign the canonical hash before package save. Add a narrow confirmed repair
operation that rewrites only legacy empty identities and removes stale derived
analysis/quality artifacts through store methods.

**Step 4: Run tests to verify they pass**

Run the same focused command.

Expected: PASS.

**Step 5: Commit**

Stage only Task 2 files and commit `fix(dedao): repair legacy ebook content identity`.

### Task 3: Initial Knowledge Release Workflow

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write the failing tests**

Extend HTTP coverage for first publication and stale-quality rejection. Extend
the Web smoke test to require a repair action for empty hashes and a first-release
button only when current quality passes.

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler.*(Repair|KnowledgeQualityAndRelease)' -count=1
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: focused assertions fail for the missing UI states.

**Step 3: Write minimal implementation**

Render lifecycle-specific actions, explicit confirmation, actionable failed
rules, and automatic refresh after analysis or repair. Reuse the existing
`POST /api/books/{book_id}/publish` endpoint.

**Step 4: Run tests to verify they pass**

Run the focused commands again.

Expected: PASS.

**Step 5: Commit**

Stage only Task 3 files and commit `feat(web): guide initial knowledge release`.

### Task 4: Controlled Agent Draft Builder

**Files:**
- Create: `backend/app/agent_package_draft.go`
- Create: `backend/app/agent_package_draft_test.go`
- Modify: `contracts/agent-package-v1.schema.json` only if a validation defect is proven

**Step 1: Write the failing tests**

Define the wished-for builder API from one published release and user choices.
Assert stable package identity, pinned release/citations, lexical citation-bound
retrieval, read-only capabilities, safety policy, bounded model policy, and
contract validation.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'TestBuildControlledAgentPackageDraft' -count=1`

Expected: FAIL because the builder is missing.

**Step 3: Write minimal implementation**

Build only the v1 read-only manifest fields required by the existing contract.
Do not introduce a new package schema or raw prompt storage.

**Step 4: Run test to verify it passes**

Run the same focused command.

Expected: PASS.

**Step 5: Commit**

Stage only Task 4 files and commit `feat(kbase): build controlled agent drafts`.

### Task 5: Publisher-Gated Agent Workflow API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go` only if a separate internal capability flag is required

**Step 1: Write the failing tests**

Add tests for draft preview, evaluation, explicit publish confirmation, missing
publisher configuration, quality/evaluation holds, idempotent replay, and proof
that responses never contain credentials.

**Step 2: Run tests to verify they fail**

Run: `go test ./backend/app -run 'TestKBaseHTTPHandlerControlledAgent' -count=1`

Expected: FAIL because the controlled workflow routes do not exist.

**Step 3: Write minimal implementation**

Add narrow authoring routes that call the existing evaluator and publisher
functions in process. Gate mutation on the server's configured publisher
authority and explicit confirmation. Return validation and evaluation artifacts
only.

**Step 4: Run tests to verify they pass**

Run the same focused command.

Expected: PASS.

**Step 5: Commit**

Stage only Task 5 files and commit `feat(kbase): add controlled agent publication workflow`.

### Task 6: Three-Step Book Agent Wizard

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Modify: `frontend-web/scripts/agent-package-web-smoke.mjs` if present; otherwise create it

**Step 1: Write the failing smoke tests**

Require release selection, policy review, evaluation review, confirmation,
failure retention, and final links to Package, Agent, and Book App routes.

**Step 2: Run tests to verify they fail**

Run the affected Node smoke scripts directly.

Expected: FAIL because the wizard states and controlled API calls are absent.

**Step 3: Write minimal implementation**

Add the three steps to the knowledge workspace Agent section. Keep defaults
read-only and display all release pins, limits, safety rules, scores, and holds.

**Step 4: Run tests to verify they pass**

Run the affected Node smoke scripts again.

Expected: PASS.

**Step 5: Commit**

Stage only Task 6 files and commit `feat(web): add controlled book agent wizard`.

### Task 7: Architecture Inventory And Documentation

**Files:**
- Modify: `docs/_generated/system-map.json`
- Modify: `README.md`
- Modify: `docs/dossiers/2026-08-08-controlled-book-agent-wizard.md`

**Step 1: Regenerate structural inventory**

Run:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

Expected: PASS with routes generated from code.

**Step 2: Document the operator workflow**

Document repair, analysis, initial release, Agent draft, evaluation, publication,
rollback/hold behavior, and every Gate decision without secrets or machine paths.

**Step 3: Run privacy checks**

Run:

```bash
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: PASS.

**Step 4: Commit**

Stage only Task 7 files and commit `docs(kbase): document controlled agent delivery`.

### Task 8: Full Verification, Deployment, And Online Acceptance

**Files:**
- Modify only files required by verified failures from this task

**Step 1: Run complete verification**

Run without output-truncating pipes:

```bash
go test ./...
node frontend/scripts/markdown-render-smoke.mjs
node frontend/scripts/book-knowledge-ui-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
cd frontend && npm run build
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all commands exit zero.

**Step 2: Review only task-owned diffs**

Confirm unrelated pre-existing worktree changes are not staged or modified by
this implementation.

**Step 3: Deploy through the repository release workflow**

Use the existing deployment scripts and health gates. Do not copy an unverified
worktree directly over production.

**Step 4: Run online acceptance**

For book `128942`, verify: repaired non-empty identity, regenerated passing
analysis/quality, immutable knowledge release, controlled Agent evaluation,
published Agent Package, grounded search/chat citations, and no credential
exposure.

**Step 5: Record Gate outcomes and commit**

Update the dossier with exact test and online evidence. Stage only the dossier
and any task-owned verified fixes, then commit `chore(kbase): verify controlled book agent release`.
