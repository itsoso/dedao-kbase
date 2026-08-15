# WeChat Account Collection Agent Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add immutable WeChat account collection Releases and a grounded collection Agent, then backfill and validate one real account in production.

**Architecture:** Keep articles as independent `BookKnowledgePackage` objects. Build a deterministic account collection candidate that pins member package hashes and citations, publish it as an immutable collection Release after a structural quality gate, and let Agent Package v3 pin that one collection Release. Runtime retrieval expands only the pinned members and resolves citations back to their original article chunks.

**Tech Stack:** Go, filesystem-backed JSON manifests with atomic writes and root locks, existing SQLite source synchronization, Wails/KBase HTTP, vanilla `frontend-web`, Node smoke scripts, systemd deployment.

---

### Task 1: Define and persist collection contracts

**Files:**
- Create: `backend/app/knowledge_collection.go`
- Create: `backend/app/knowledge_collection_test.go`

**Step 1: Write failing contract tests**

Add tests for canonical collection definitions, unique source identity,
deterministic candidate ordering and hashes, member limits, duplicate member
rejection, and invalid identifiers. Use synthetic packages only.

The wished-for API is:

```go
definition, err := store.SaveKnowledgeCollection(KnowledgeCollectionDefinition{
    SchemaVersion: KnowledgeCollectionDefinitionSchemaVersion,
    CollectionID: "wechat-account-fixture",
    Title: "Fixture account knowledge",
    SourceType: "wechat_mp_article",
    SourceAccountKey: "account-fixture",
    SourceAccount: "Fixture account",
    Enabled: true,
})
candidate, err := store.BuildKnowledgeCollectionCandidate(definition.CollectionID)
```

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestKnowledgeCollection' -count=1
```

Expected: FAIL because collection types and methods do not exist.

**Step 3: Implement minimal contracts and atomic storage**

Implement definition, member, exclusion, candidate, quality, Release, and
manifest structs. Store definitions, candidates, quality reports, immutable
Releases, and their manifest under a dedicated collection directory below the
knowledge root. Reuse existing atomic JSON writes and root lock order. Bound
the first version to 500 members and bounded exclusion messages.

**Step 4: Verify GREEN**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_collection.go backend/app/knowledge_collection_test.go
git commit -m "feat(kbase): persist knowledge collections"
```

### Task 2: Build account-scoped candidates and deterministic quality reports

**Files:**
- Modify: `backend/app/knowledge_collection.go`
- Modify: `backend/app/knowledge_collection_test.go`
- Modify: `backend/app/knowledge_catalog.go`
- Modify: `backend/app/knowledge_catalog_test.go`
- Modify: `backend/app/source_ingest_test.go`

**Step 1: Write failing membership tests**

Cover exact `(source_type, source_account_key)` filtering from the knowledge
catalog, account-name changes,
foreign-account exclusion, missing content hashes, missing or cross-book
citations, deterministic rebuilds, changed article hashes, and partial
candidate status. Add a regression assertion that canonical source ingestion
persists the account key required by collection membership.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestKnowledgeCollection|TestSourceArticleIngest' -count=1
```

Expected: FAIL on missing account-scoped candidate behavior.

**Step 3: Implement candidate construction and quality evaluation**

Add a bounded catalog query for current versions by source account. Load only
the packages referenced by those catalog versions. Pin the package content
hash, publication timestamp, source item identity, and citation IDs. Return
bounded exclusions with stable codes. Evaluate structural rules without
calling an LLM. Force `evidence_only` for the WeChat account collection type.

**Step 4: Verify GREEN**

Run the focused command. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_collection.go backend/app/knowledge_collection_test.go backend/app/knowledge_catalog.go backend/app/knowledge_catalog_test.go backend/app/source_ingest_test.go
git commit -m "feat(kbase): build account collection candidates"
```

### Task 3: Publish immutable collection Releases

**Files:**
- Modify: `backend/app/knowledge_collection.go`
- Modify: `backend/app/knowledge_collection_test.go`

**Step 1: Write failing publication tests**

Test quality-pass requirement, candidate-hash freshness, deterministic Release
identity, immutable replay, superseding without mutation, stale member failure,
and restart persistence.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestKnowledgeCollection.*Release' -count=1
```

Expected: FAIL because publication is not implemented.

**Step 3: Implement publication**

Add `PublishKnowledgeCollection(collectionID string)` and Release load/list
methods. Derive the Release ID from schema version, collection identity,
candidate hash, member pins, quality decision, and usage policy. Never overwrite
an existing Release file.

**Step 4: Verify GREEN and restart persistence**

Run the focused command. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_collection.go backend/app/knowledge_collection_test.go
git commit -m "feat(kbase): publish collection releases"
```

### Task 4: Expose authenticated collection APIs

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/system-map/main.go` only if the generator requires a new route category
- Modify: `docs/_generated/system-map.json`

**Step 1: Write failing HTTP tests**

Cover:

```text
GET  /api/knowledge/collections
POST /api/knowledge/collections
GET  /api/knowledge/collections/{collection_id}
POST /api/knowledge/collections/{collection_id}/build
POST /api/knowledge/collections/{collection_id}/publish
GET  /api/knowledge/collection-releases/{release_id}
```

Assert Bearer authentication, request-size limits, method rejection, not-found
mapping, quality-gate conflict, and no source credentials in responses.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestKBaseHTTPKnowledgeCollection' -count=1
```

Expected: FAIL with missing routes.

**Step 3: Implement handlers**

Use existing admin authentication and bounded JSON helpers. Return explicit
candidate, quality, published Release, and exclusion summaries.

**Step 4: Regenerate and verify the system map**

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
go test ./backend/app -run 'TestKBaseHTTPKnowledgeCollection' -count=1
```

Expected: all commands exit 0.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/system-map/main.go docs/_generated/system-map.json
git commit -m "feat(kbase): expose knowledge collection API"
```

Omit `cmd/system-map/main.go` from staging if it did not change.

### Task 5: Add Agent Package v3 collection scope

**Files:**
- Modify: `backend/app/agent_package.go`
- Modify: `backend/app/agent_package_test.go`
- Create: `backend/app/agent_collection_package.go`
- Create: `backend/app/agent_collection_package_test.go`
- Modify: `backend/app/agent_package_store.go`

**Step 1: Write failing schema and draft tests**

Define the wished-for scope:

```go
type AgentPackageCollectionRef struct {
    ReleaseID string `json:"release_id"`
    ContentHash string `json:"content_hash"`
}
```

Test that v1/v2 remain compatible, v3 requires exactly one collection Release,
the collection hash is pinned, source type is enforced, the package uses
read-only tools and `evidence_only`, and a stale or missing collection Release
is rejected.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestAgentPackage.*Collection|TestBuildControlledCollectionAgent' -count=1
```

Expected: FAIL because collection Agent scope does not exist.

**Step 3: Implement the minimal v3 extension and draft builder**

Add `collection_releases` without changing the meaning of `releases` for v1/v2.
The first version permits one collection Release. Build a deterministic draft
ID from the collection identity and retain the current model, cost, timeout,
tool, prompt, UI, and evaluation policy patterns.

**Step 4: Verify GREEN and backward compatibility**

```bash
go test ./backend/app -run 'TestAgentPackage|TestBuildControlledCollectionAgent' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_package.go backend/app/agent_package_test.go backend/app/agent_collection_package.go backend/app/agent_collection_package_test.go backend/app/agent_package_store.go
git commit -m "feat(agent): add collection package scope"
```

### Task 6: Search pinned collection members and resolve citations

**Files:**
- Modify: `backend/app/agent_runtime.go`
- Modify: `backend/app/agent_runtime_test.go`
- Modify: `backend/app/agent_trace.go`
- Modify: `backend/app/agent_trace_test.go`
- Modify: `backend/app/book_mcp.go`

**Step 1: Write failing runtime tests**

Create two synthetic account member packages plus one foreign package. Assert
global top-K chunk ranking, no foreign result, pinned hash validation,
collection/member IDs in evidence, citation allowlist enforcement,
cross-article answers, out-of-scope abstention, and runtime trace provenance.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestAgent.*CollectionRuntime|TestAgentTrace.*Collection' -count=1
```

Expected: FAIL on missing collection runtime support.

**Step 3: Implement scoped chunk retrieval**

Load and verify the collection Release once, then search only its pinned member
packages. Rank results globally and truncate to `max_context_chunks`. Convert
matching chunks to Agent evidence with their original citation IDs; do not
invent analysis claims. Resolve citations from the pinned member package and
record collection plus selected member hashes in the trace.

**Step 4: Verify GREEN**

Run the focused command. Expected: PASS with no race or mutation failures.

**Step 5: Commit**

```bash
git add backend/app/agent_runtime.go backend/app/agent_runtime_test.go backend/app/agent_trace.go backend/app/agent_trace_test.go backend/app/book_mcp.go
git commit -m "feat(agent): search collection releases"
```

### Task 7: Add trusted evaluation and publication gates

**Files:**
- Modify: `backend/app/agent_package_evaluation.go`
- Modify: `backend/app/agent_package_evaluation_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing evaluation tests**

Cover collection membership integrity, stale hashes, account leakage,
unresolved citations, forbidden tools, required abstention, and one successful
grounded runtime probe. Verify that failed evaluation cannot publish and the
previous package version remains active.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestAgentPackage.*Collection.*Evaluation|TestKBaseHTTP.*CollectionAgent' -count=1
```

Expected: FAIL on absent collection evaluation cases and draft endpoint.

**Step 3: Implement gates and draft HTTP route**

Add `POST /api/agent-packages/collection-draft`. Reuse the existing evaluation
and publish endpoints after teaching their validation path about v3 collection
scope. Do not add an auto-publish path.

**Step 4: Verify GREEN**

Run the focused command. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_package_evaluation.go backend/app/agent_package_evaluation_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(agent): gate collection agent publication"
```

### Task 8: Make throttling resumable without bypassing operator blocks

**Files:**
- Modify: `backend/app/source_sync.go`
- Modify: `backend/app/source_scheduler.go`
- Modify: `backend/app/source_scheduler_test.go`
- Modify: `backend/app/wechat_agent.go`
- Modify: `backend/app/wechat_agent_test.go`

**Step 1: Write failing cooldown tests**

Test that upstream throttling records a bounded cooldown and becomes eligible
after it expires, while authentication expiry, verification challenges, and
permission failures remain blocked. Verify cursor preservation and that no
active run is duplicated.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceScheduler.*Throttle|TestWeChat.*Throttle' -count=1
```

Expected: FAIL because throttle is currently treated as permanently blocked.

**Step 3: Implement typed cooldown behavior**

Persist a non-secret retry timestamp or typed error detail on the run. Scheduler
retries only after the cooldown. Do not retry login, verification, forbidden,
or malformed-content failures automatically.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'TestSourceScheduler|TestWeChat' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/source_sync.go backend/app/source_scheduler.go backend/app/source_scheduler_test.go backend/app/wechat_agent.go backend/app/wechat_agent_test.go
git commit -m "fix(wechat): resume after throttle cooldown"
```

### Task 9: Build the Chinese collection workspace

**Files:**
- Modify: `frontend-web/index.html`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Create: `frontend-web/scripts/wechat-collection-agent-smoke.mjs`

**Step 1: Write the failing DOM smoke**

Assert routes and Chinese labels for the collection summary, acquisition
health, membership diff, exclusions, quality rules, explicit Release publish,
Agent draft/evaluate/publish, member links, and rollback state. Assert no
auto-publish call and no large empty hero area.

**Step 2: Verify RED**

```bash
node frontend-web/scripts/wechat-collection-agent-smoke.mjs
```

Expected: FAIL because the workspace is absent.

**Step 3: Implement the workspace**

Add `/knowledge/collections/:collectionID` and link it from the selected source
subscription. Use dense, responsive tables for members and failures, bounded
summaries for status, and existing session-aware API helpers for mutations.

**Step 4: Verify GREEN and syntax**

```bash
node frontend-web/scripts/wechat-collection-agent-smoke.mjs
node --check frontend-web/app.js
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/index.html frontend-web/app.js frontend-web/styles.css frontend-web/scripts/wechat-collection-agent-smoke.mjs
git commit -m "feat(web): add collection agent workspace"
```

### Task 10: Full verification and privacy review

**Files:**
- Modify: `docs/dossiers/2026-08-12-wechat-account-collection-agent.md`
- Modify: `docs/plans/2026-08-12-wechat-account-collection-agent-design.md` only if implementation decisions changed

**Step 1: Run focused and full gates without output-masking pipes**

```bash
go test ./... -count=1
go vet ./...
go test -race ./backend/app ./cmd/kbase-server ./cmd/source-agent -count=1
cd frontend && npm run build
cd ..
node frontend-web/scripts/wechat-collection-agent-smoke.mjs
node frontend-web/scripts/agent-console-ui-smoke.mjs
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: every command exits 0. Record any existing compiler warnings
separately; do not convert a warning into a pass claim.

**Step 2: Review security and integrity boundaries**

Inspect the final diff for authentication coverage, path safety, member-count
bounds, hash validation, citation resolution, cross-account leakage, runtime
trace privacy, medical `evidence_only`, and explicit publication.

**Step 3: Update the Dossier and commit only task files**

```bash
git add docs/dossiers/2026-08-12-wechat-account-collection-agent.md docs/plans/2026-08-12-wechat-account-collection-agent-design.md
git commit -m "docs(kbase): record collection agent gates"
```

### Task 11: Deploy from a clean reviewed branch

**Files:**
- Modify: `docs/dossiers/2026-08-12-wechat-account-collection-agent.md`

**Step 1: Confirm deploy input and rollback point**

Verify the branch contains only reviewed commits, the target mainline relation
is understood, and the production backup identifier is recorded. Do not deploy
from the dirty primary worktree.

**Step 2: Build and deploy with the existing direct cutover contract**

Use the repository's documented release workflow and
`scripts/kbase-direct-deployment-cutover.sh`. Preserve the previous server and
static artifacts for rollback.

**Step 3: Verify G5**

Check `/health`, deployed revision, authenticated collection APIs, static asset
hashes, service active state, restart count, and recent warning/error logs.

Expected: exact revision, HTTP success, active services, no new restart loop,
and no unexplained warning/error.

### Task 12: Backfill, publish, and validate the real collection Agent

**Files:**
- Modify: `docs/dossiers/2026-08-12-wechat-account-collection-agent.md`

**Step 1: Run a bounded production synchronization**

Start with a small page, verify skip/update/new accounting and cursor progress,
then resume bounded runs until the visible history boundary is reached. Respect
cooldowns. Never log article bodies or credentials.

**Step 2: Reconcile completeness**

Compare discovered identities, imported packages, failures, exclusions, and
the stable terminal cursor. Retry transient item failures. Record the observed
visible total and unresolved exclusions; do not claim inaccessible articles.

**Step 3: Create and inspect the collection candidate**

Verify member count, no foreign-account members, pinned hashes and citations,
quality decision, and candidate-versus-published diff.

**Step 4: Publish the collection Release explicitly**

Record the Release ID and content hash. Reload it through the public admin API
and verify immutability.

**Step 5: Build, evaluate, and publish the Agent explicitly**

Create a `1.0.0` collection Agent draft, run the trusted evaluation suite, and
publish only if every required metric passes. Record the package ID, version,
hash, and evaluation identity.

**Step 6: Execute real online questions**

Ask at least three questions that require different member articles, plus one
out-of-scope question. Verify returned evidence belongs to the pinned
collection, every cited ID resolves to an original member chunk, the Agent
abstains outside scope, and browser/server logs remain clean.

**Step 7: Enable daily incremental synchronization**

Set the existing account subscription to `interval:86400`. Confirm that a new
or changed article creates a collection candidate but does not silently mutate
the published Release or Agent.

**Step 8: Record G6 and rollback instructions**

Update the Dossier with actual counts, failed/excluded items, Release and Agent
identities, production revision, real-query results, service health, and the
exact rollback target. Mark shipped only when every required verification is
green.
