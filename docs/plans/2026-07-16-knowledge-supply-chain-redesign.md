# Knowledge Supply Chain Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn KBase into a reusable knowledge supply chain that can collect, normalize, search, evaluate, release, and deliver traceable knowledge to the health consumer first, then Proofroom and other consumers.

**Architecture:** Preserve the current KBase server, filesystem artifacts, SQLite catalog, local source agents, release feed, receipts, lineage, impact, and review cockpit. Add a source connector contract, hybrid-search-ready index model, health consumer release contract, evaluation harness, and change-impact planning without breaking existing routes.

**Tech Stack:** Go 1.23, SQLite, vanilla JavaScript/CSS, Node smoke scripts, JSON fixtures, systemd deployment.

---

## Checkpoint 1: Source Connector Contract

### Task 1: Define a common connector interface

**Files:**
- Create: `backend/app/source_connector.go`
- Create: `backend/app/source_connector_test.go`

**Step 1: Write failing tests**

Cover connector name validation, capability validation, checkpoint monotonicity,
stable source document identity, content hash binding, and invalid license
scope rejection.

Run:

```bash
go test ./backend/app -run SourceConnector -count=1
```

Expected: FAIL because the connector contract does not exist.

**Step 2: Implement the contract**

Add:

- `SourceConnector`
- `SourceConnectorCapabilities`
- `SourceFetchRequest`
- `SourceFetchPage`
- `SourceCheckpoint`
- `SourceDocumentEnvelope`

The contract should use canonical source metadata and no source-specific fields
outside `Metadata`.

**Step 3: Verify**

```bash
go test ./backend/app -run SourceConnector -count=1
git diff --check
```

**Step 4: Commit**

```bash
git add backend/app/source_connector.go backend/app/source_connector_test.go
git commit -m "feat(kbase): define source connector contract"
```

### Task 2: Adapt existing source ingest to connector documents

**Files:**
- Modify: `backend/app/source_ingest.go`
- Modify: `backend/app/source_ingest_test.go`
- Modify: `backend/app/source_agent_runner.go`

**Step 1: Write failing adapter tests**

Cover conversion from `SourceDocumentEnvelope` to the existing
`SourceArticleEnvelope`, idempotency preservation, source URL validation,
metadata preservation, and unchanged target book IDs.

Run:

```bash
go test ./backend/app -run 'SourceConnector|SourceIngest' -count=1
```

**Step 2: Implement an adapter path**

Do not remove existing HTTP payloads. Add helper conversion so current WC Plus
and WeChat local agents can gradually move to the connector contract.

**Step 3: Verify**

```bash
go test ./backend/app -run 'SourceConnector|SourceIngest|SourceAgentRunner' -count=1
```

**Step 4: Commit**

```bash
git add backend/app/source_ingest.go backend/app/source_ingest_test.go backend/app/source_agent_runner.go
git commit -m "feat(kbase): adapt source ingest to connector documents"
```

## Checkpoint 2: Search Foundation

### Task 3: Add a rebuildable knowledge search index model

**Files:**
- Create: `backend/app/knowledge_search_index.go`
- Create: `backend/app/knowledge_search_index_test.go`
- Modify: `backend/app/knowledge_catalog.go`

**Step 1: Write failing index tests**

Cover rebuild from packages, keyword matching on title/chunk/claim, source type
filtering, freshness filtering, consumer policy filtering, and stable ranking.

Run:

```bash
go test ./backend/app -run KnowledgeSearchIndex -count=1
```

**Step 2: Implement SQLite FTS5-backed records**

Start with keyword search and explicit metadata filters. Keep embedding fields
nullable so semantic search can be added later without a migration break.

**Step 3: Verify**

```bash
go test ./backend/app -run 'KnowledgeSearchIndex|KnowledgeCatalog|BookKnowledgeSearch' -count=1
```

**Step 4: Commit**

```bash
git add backend/app/knowledge_search_index.go backend/app/knowledge_search_index_test.go backend/app/knowledge_catalog.go
git commit -m "feat(kbase): index knowledge packages for search"
```

## Checkpoint 3: Health Consumer Supply Contract

### Task 4: Add health release feed DTOs and fixtures

**Files:**
- Create: `backend/app/health_kb_feed.go`
- Create: `backend/app/health_kb_feed_test.go`
- Create: `contracts/health-kb-release-v1.schema.json`
- Create: `contracts/fixtures/health-kb-feed-page.json`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing contract tests**

Cover evidence-only policy, release ID, source lineage, claim citations,
content hash, freshness, cursor pagination, idempotent replay, and auth.

Run:

```bash
go test ./backend/app -run HealthKB -count=1
```

**Step 2: Implement the health feed**

Expose a scoped route such as:

```text
GET /api/consumers/health/releases?after={cursor}&limit={n}
```

The payload must not include raw source credentials, raw prompts, user records,
or local machine paths.

**Step 3: Verify**

```bash
go test ./backend/app -run 'HealthKB|KnowledgeFeed|KbaseHTTP' -count=1
bash scripts/privacy-smoke.sh
```

**Step 4: Commit**

```bash
git add backend/app/health_kb_feed.go backend/app/health_kb_feed_test.go contracts/health-kb-release-v1.schema.json contracts/fixtures/health-kb-feed-page.json backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): expose health knowledge feed"
```

## Checkpoint 4: Evaluation Harness

### Task 5: Add retrieval and answer evaluation skeleton

**Files:**
- Create: `backend/app/knowledge_eval.go`
- Create: `backend/app/knowledge_eval_test.go`
- Create: `contracts/fixtures/knowledge-eval-suite.json`
- Create: `scripts/knowledge-eval-smoke.sh`

**Step 1: Write failing evaluation tests**

Cover synthetic evaluation suites with expected chunk IDs, citation coverage,
answer faithfulness flags, zero-hit gaps, and deterministic score output.

Run:

```bash
go test ./backend/app -run KnowledgeEval -count=1
```

**Step 2: Implement deterministic scoring**

Start with no-LLM metrics:

- retrieval hit rate;
- citation coverage;
- unsupported answer markers;
- stale source count;
- zero-hit gap fingerprints.

**Step 3: Verify**

```bash
go test ./backend/app -run 'KnowledgeEval|KnowledgeSearchIndex' -count=1
bash scripts/knowledge-eval-smoke.sh
```

**Step 4: Commit**

```bash
git add backend/app/knowledge_eval.go backend/app/knowledge_eval_test.go contracts/fixtures/knowledge-eval-suite.json scripts/knowledge-eval-smoke.sh
git commit -m "feat(kbase): evaluate knowledge retrieval quality"
```

## Checkpoint 5: Impact Automation

### Task 6: Plan rebuilds from source changes

**Files:**
- Create: `backend/app/knowledge_rebuild_plan.go`
- Create: `backend/app/knowledge_rebuild_plan_test.go`
- Modify: `backend/app/knowledge_impact.go`
- Modify: `backend/app/knowledge_review.go`

**Step 1: Write failing impact tests**

Cover source content change, changed claims, affected health releases, pending
consumer receipts, stale evaluation scores, and safe no-op plans.

Run:

```bash
go test ./backend/app -run KnowledgeRebuildPlan -count=1
```

**Step 2: Implement plan generation**

The planner should recommend rebuild, reevaluate, republish, or notify consumer.
It should not automatically publish new releases.

**Step 3: Verify**

```bash
go test ./backend/app -run 'KnowledgeRebuildPlan|KnowledgeImpact|KnowledgeReview' -count=1
```

**Step 4: Commit**

```bash
git add backend/app/knowledge_rebuild_plan.go backend/app/knowledge_rebuild_plan_test.go backend/app/knowledge_impact.go backend/app/knowledge_review.go
git commit -m "feat(kbase): plan knowledge rebuild impact"
```

## Checkpoint 6: Control Plane UI

### Task 7: Surface supply-chain status in the web control plane

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write failing smoke coverage**

Assert that the Book Knowledge page exposes:

- source connector status;
- search index freshness;
- health feed status;
- evaluation status;
- rebuild recommendations.

Run:

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

**Step 2: Implement the UI**

Keep the current left-right layout. Add compact operational cards and URL-safe
links rather than another tab row.

**Step 3: Verify**

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
cd frontend && npm run build
```

**Step 4: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/book-knowledge-web-smoke.mjs
git commit -m "feat(kbase): show knowledge supply status"
```

## Checkpoint 7: Release Validation

### Task 8: Run full verification and deploy

**Commands:**

```bash
go test ./...
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
bash scripts/privacy-smoke.sh
git diff --check
```

Deploy only from a clean, pushed branch. After deployment, verify:

```text
/health
/api/knowledge/feed
/api/consumers/health/releases
/api/knowledge/impact
/book-knowledge
```

**Commit/PR:**

Open a PR with links to the design, implementation plan, tests, and production
smoke evidence. Merge only after G3 tests, G4 review, G5 deploy health, and G6
online verification pass.
