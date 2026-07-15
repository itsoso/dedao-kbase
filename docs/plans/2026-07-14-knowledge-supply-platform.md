# Knowledge Supply Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the existing source-to-release loop into an observable, versioned knowledge supply contract for a reviewed health consumer.

**Architecture:** Preserve immutable filesystem artifacts and existing execution owners. Add a rebuildable SQLite catalog, derived pipeline projection, cursor-based release feed, idempotent delivery receipts, lineage reads, and privacy-safe impact/gap aggregates. KBase remains the authoring/release plane; consumer domain review and serving remain external.

**Tech Stack:** Go 1.23, SQLite, vanilla JavaScript/CSS, JSON Schema, Node smoke scripts, systemd deployment.

---

## Checkpoint 1: Foundation And Drift Control

### Task 1: Generate the system map from code

**Files:**
- Create: `cmd/system-map/main.go`
- Create: `cmd/system-map/main_test.go`
- Create: `docs/system-map/INDEX.md`
- Create: `docs/system-map/product-map.md`
- Create: `docs/_generated/system-map.json`
- Create: `scripts/system-map-smoke.sh`
- Modify: `README.md`

**Step 1: Write the failing generator tests**

Test a temporary repository fixture and assert that the generator discovers:

- `cmd/*/main.go` command surfaces;
- authenticated and unauthenticated HTTP path literals from the Go AST;
- source adapter operation constants;
- durable storage object types;
- stable sorted JSON output with a schema version.

Do not assert hand-maintained architecture counts in narrative Markdown.

**Step 2: Verify RED**

Run:

```bash
go test ./cmd/system-map -v
```

Expected: FAIL because the generator does not exist.

**Step 3: Implement the AST-based generator**

Use `go/parser`, `go/ast`, and `go/token`; do not grep source text. Emit only
structural inventory and relative paths. Reject absolute paths, secrets, and
content bodies.

Example output shape:

```json
{
  "schema_version": "1",
  "commands": [],
  "http_routes": [],
  "source_operations": [],
  "durable_objects": []
}
```

`scripts/system-map-smoke.sh` regenerates into a temporary file and compares it
with `docs/_generated/system-map.json`.

**Step 4: Verify GREEN**

Run:

```bash
go test ./cmd/system-map -v
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS and the generated file contains no machine-specific path.

**Step 5: Commit**

```bash
git add cmd/system-map scripts/system-map-smoke.sh docs/system-map docs/_generated/system-map.json README.md
git commit -m "docs(kbase): generate system architecture map"
```

### Task 2: Add machine-readable knowledge contracts

**Files:**
- Create: `contracts/knowledge-release-v1.schema.json`
- Create: `contracts/knowledge-feed-v1.schema.json`
- Create: `contracts/delivery-receipt-v1.schema.json`
- Create: `contracts/fixtures/release-minimal.json`
- Create: `contracts/fixtures/feed-page.json`
- Create: `contracts/fixtures/delivery-receipt.json`
- Create: `backend/app/knowledge_contract_test.go`
- Modify: `backend/app/knowledge_release.go`

**Step 1: Write failing compatibility tests**

Load fixtures and assert round-trip compatibility with `KnowledgeRelease` and
new feed/receipt DTOs. Cover required release identity, content hash, citations,
usage policy, schema version, cursor, and idempotency key. Assert that unknown
optional fields survive consumer fixture validation but missing required fields
fail.

**Step 2: Verify RED**

```bash
go test ./backend/app -run KnowledgeContract -v
```

Expected: FAIL because feed and receipt contracts do not exist.

**Step 3: Implement the minimal contract layer**

Add explicit `schema_version` fields to new envelopes without changing existing
release identity or URLs. Keep fixtures synthetic and free of source text.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run KnowledgeContract -v
bash scripts/privacy-smoke.sh
git diff --check
```

**Step 5: Commit**

```bash
git add contracts backend/app/knowledge_contract_test.go backend/app/knowledge_release.go
git commit -m "feat(kbase): define knowledge supply contracts"
```

## Checkpoint 2: Rebuildable Catalog And Pipeline Projection

### Task 3: Build the canonical source and content catalog

**Files:**
- Create: `backend/app/knowledge_catalog.go`
- Create: `backend/app/knowledge_catalog_test.go`
- Modify: `backend/app/source_ingest.go`
- Modify: `backend/app/source_ingest_test.go`
- Modify: `cmd/kbase-server/main.go`

**Step 1: Write failing catalog tests**

Cover:

- stable source identity from adapter plus remote object key;
- immutable content version from normalized content hash;
- repeated ingest as exact duplicate without duplicate version rows;
- a changed hash superseding the prior version;
- probable-duplicate grouping without automatic deletion;
- complete rebuild from current package manifests and source-run records.

**Step 2: Verify RED**

```bash
go test ./backend/app -run KnowledgeCatalog -v
```

**Step 3: Implement the catalog**

Use SQLite tables for `knowledge_sources`, `knowledge_content_versions`, and
`knowledge_duplicate_groups`. Store identifiers, hashes, provenance metadata,
relative artifact references, license scope, and timestamps only. Make rebuild
transactional and safe to rerun.

Hook successful source ingest after durable package persistence. A catalog write
failure must fail visibly but must not delete the durable package.

**Step 4: Verify GREEN and recovery**

```bash
go test ./backend/app -run 'KnowledgeCatalog|SourceIngest' -v
go test -race ./backend/app -run 'KnowledgeCatalog|SourceIngest' -count=1
```

Delete only the test catalog, rebuild it, and assert identical logical records.

**Step 5: Commit**

```bash
git add backend/app/knowledge_catalog.go backend/app/knowledge_catalog_test.go backend/app/source_ingest.go backend/app/source_ingest_test.go cmd/kbase-server/main.go
git commit -m "feat(kbase): catalog source content versions"
```

### Task 4: Project one pipeline state across existing execution owners

**Files:**
- Create: `backend/app/knowledge_pipeline.go`
- Create: `backend/app/knowledge_pipeline_test.go`
- Modify: `backend/app/source_sync.go`
- Modify: `backend/app/book_analysis.go`
- Modify: `backend/app/book_quality.go`
- Modify: `backend/app/knowledge_release.go`
- Modify: `backend/app/knowledge_reverification.go`

**Step 1: Write failing projection tests**

Create fixtures for each stage:

```text
collected -> normalized -> analyzed -> verified -> candidate -> published
```

Assert deterministic current stage, input fingerprint, output reference,
attempts, timestamps, and public error code. Assert that a failed later run does
not erase the last published release and a stale artifact is not projected as
current.

**Step 2: Verify RED**

```bash
go test ./backend/app -run KnowledgePipeline -v
```

**Step 3: Implement a derived projection**

Read existing source runs, package manifests, analysis manifests, quality
reports, releases, and reverification tasks. Do not create a second scheduler or
copy raw errors. Persist only the latest projection in the rebuildable catalog.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'KnowledgePipeline|KnowledgeReverification|BookQuality|KnowledgeRelease' -v
go test -race ./backend/app -run 'KnowledgePipeline|KnowledgeReverification' -count=1
```

**Step 5: Commit**

```bash
git add backend/app/knowledge_pipeline.go backend/app/knowledge_pipeline_test.go backend/app/source_sync.go backend/app/book_analysis.go backend/app/book_quality.go backend/app/knowledge_release.go backend/app/knowledge_reverification.go
git commit -m "feat(kbase): project knowledge pipeline state"
```

## Checkpoint 3: Reliable Release Delivery

### Task 5: Add the incremental release feed

**Files:**
- Create: `backend/app/knowledge_feed.go`
- Create: `backend/app/knowledge_feed_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `contracts/fixtures/feed-page.json`

**Step 1: Write failing feed tests**

Cover stable cursor order, `limit`, source kind, usage policy, changed-since,
book ID, no duplicate replay after cursor advancement, invalid cursor response,
authentication, and method handling. Feed only immutable published releases.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'KnowledgeFeed|HTTPHandlerKnowledgeFeed' -v
```

**Step 3: Implement `GET /api/knowledge/feed`**

Use a cursor containing ordered release time plus release ID, encoded as an
opaque value. Filters run before pagination. Return `schema_version`, releases,
`next_cursor`, and `has_more`. Never return drafts or quality-rejected records.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'KnowledgeFeed|HTTPHandlerKnowledgeFeed' -v
go test -race ./backend/app -run KnowledgeFeed -count=1
```

**Step 5: Commit**

```bash
git add backend/app/knowledge_feed.go backend/app/knowledge_feed_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go contracts/fixtures/feed-page.json
git commit -m "feat(kbase): expose incremental release feed"
```

### Task 6: Record idempotent delivery receipts

**Files:**
- Create: `backend/app/knowledge_delivery.go`
- Create: `backend/app/knowledge_delivery_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `contracts/fixtures/delivery-receipt.json`

**Step 1: Write failing receipt tests**

Cover `imported`, `held`, `rejected`, and `failed`; idempotent replay; conflicting
payload under one idempotency key; unknown release; bounded reason code;
consumer identifier validation; and rejection of unexpected personal-data-like
fields.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'KnowledgeDelivery|HTTPHandlerDeliveryReceipt' -v
```

**Step 3: Implement receipt persistence and API**

Add:

```text
POST /api/knowledge/releases/{release_id}/receipts
```

Persist release ID, scoped consumer ID, disposition, imported fingerprint,
reason code, idempotency key, and timestamp in SQLite. Disallow arbitrary notes,
queries, answers, and user identifiers.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'KnowledgeDelivery|HTTPHandlerDeliveryReceipt' -v
go test -race ./backend/app -run KnowledgeDelivery -count=1
```

**Step 5: Commit**

```bash
git add backend/app/knowledge_delivery.go backend/app/knowledge_delivery_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go contracts/fixtures/delivery-receipt.json
git commit -m "feat(kbase): record consumer delivery receipts"
```

### Task 7: Expose end-to-end lineage

**Files:**
- Create: `backend/app/knowledge_lineage.go`
- Create: `backend/app/knowledge_lineage_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Create: `frontend-web/scripts/knowledge-supply-ui-smoke.mjs`

**Step 1: Write failing backend and UI tests**

Given a source/content version, chunk, claim, release, and receipt, assert one
bounded graph response with typed nodes and edges. Assert no source body,
filesystem path, token, or consumer payload appears. Add UI smoke markers for a
URL-addressable lineage drawer.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'KnowledgeLineage|HTTPHandlerKnowledgeLineage' -v
node frontend-web/scripts/knowledge-supply-ui-smoke.mjs
```

**Step 3: Implement lineage and UI**

Add `GET /api/knowledge/lineage/{object_id}`. Render lineage inside the Release
workspace using identifiers, hashes, stage, time, and disposition only. Keep the
default view compact; represent the selected object in the URL.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'KnowledgeLineage|HTTPHandlerKnowledgeLineage' -v
node frontend-web/scripts/knowledge-supply-ui-smoke.mjs
node --check frontend-web/app.js
```

**Step 5: Commit**

```bash
git add backend/app/knowledge_lineage.go backend/app/knowledge_lineage_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go frontend-web/app.js frontend-web/styles.css frontend-web/scripts/knowledge-supply-ui-smoke.mjs
git commit -m "feat(kbase): expose release lineage"
```

## Checkpoint 4: Health Pilot And Impact Loop

### Task 8: Add impact and privacy-safe gap projections

**Files:**
- Create: `backend/app/knowledge_impact.go`
- Create: `backend/app/knowledge_impact_test.go`
- Modify: `backend/app/knowledge_feedback.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/knowledge-supply-ui-smoke.mjs`

**Step 1: Write failing aggregate tests**

Cover release imports, receipt failures, used claims, zero hits, invalidating
feedback, oldest failure age, and reverification age. A gap stores only a
consumer-provided opaque fingerprint, category, count, and timestamps; reject
raw query or personal record fields.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'KnowledgeImpact|KnowledgeGap' -v
```

**Step 3: Implement read models**

Add:

```text
GET /api/knowledge/impact
GET /api/knowledge/gaps
```

Project metrics from releases, receipts, feedback, and reverification tasks.
Render the Impact workspace with operational tables and trend-free current
counts/age first; do not invent analytics history before durable snapshots
exist.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'KnowledgeImpact|KnowledgeGap|KnowledgeFeedback' -v
node frontend-web/scripts/knowledge-supply-ui-smoke.mjs
node --check frontend-web/app.js
```

**Step 5: Commit**

```bash
git add backend/app/knowledge_impact.go backend/app/knowledge_impact_test.go backend/app/knowledge_feedback.go backend/app/kbase_http.go backend/app/kbase_http_test.go frontend-web/app.js frontend-web/styles.css frontend-web/scripts/knowledge-supply-ui-smoke.mjs
git commit -m "feat(kbase): surface knowledge supply impact"
```

## Checkpoint 5: Consumer Contract And Rollout

### Task 9: Validate the first consumer without coupling runtimes

**Files:**
- Create: `docs/operations/knowledge-consumer-contract.md`
- Create: `scripts/knowledge-consumer-contract-smoke.sh`
- Modify: `README.md`
- Modify: `docs/dossiers/2026-07-14-knowledge-supply-platform.md`

**Step 1: Write a failing black-box contract smoke**

The smoke starts a temporary KBase with synthetic releases, consumes the feed
with a persisted cursor, validates contract fixtures, posts an imported receipt,
replays it idempotently, posts bounded feedback, and verifies impact/lineage.
It must not depend on a downstream repository or network service.

**Step 2: Verify RED**

```bash
bash scripts/knowledge-consumer-contract-smoke.sh
```

**Step 3: Implement the smoke and run downstream companion tests**

Document the consumer algorithm:

1. Read feed after durable cursor.
2. Validate release schema and usage policy.
3. Import into a candidate workspace transaction.
4. Advance cursor only after successful import.
5. Post an idempotent receipt.
6. Perform domain review before reviewed serving publication.
7. Return claim-level usage/rejection feedback asynchronously.

Create a separate consumer-side dossier for its importer and review tests; do
not add consumer runtime code to this repository.

**Step 4: Run full G3 and G4 gates**

```bash
go test ./...
go vet ./...
go test -race ./backend/app ./cmd/kbase-server ./cmd/source-agent -count=1
cd frontend && npm ci && npm run build
cd ..
for script in frontend-web/scripts/*.mjs; do node "$script"; done
bash scripts/system-map-smoke.sh
bash scripts/knowledge-consumer-contract-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS. Record pre-existing dependency warnings separately; do not
hide or auto-fix them in this feature.

**Step 5: Commit**

```bash
git add docs/operations/knowledge-consumer-contract.md scripts/knowledge-consumer-contract-smoke.sh README.md docs/dossiers/2026-07-14-knowledge-supply-platform.md
git commit -m "docs(kbase): define consumer rollout contract"
```

### Task 10: Deploy one checkpoint at a time

**Files:**
- Modify after evidence: `docs/dossiers/2026-07-14-knowledge-supply-platform.md`

**Step 1: Create a PR for the completed checkpoint**

Include exact tests, contract changes, migrations, rollback, and screenshots for
control-plane changes. Do not combine later checkpoints into a red Gate.

**Step 2: Deploy from clean merged main**

Back up the server binary, static Web directory, and catalog DB. Build with the
pinned Go toolchain, replace atomically, restart systemd, and wait for `/health`.

**Step 3: Verify production non-destructively**

- public health returns 200;
- unauthenticated admin/feed/receipt routes return 401;
- authenticated feed pagination is stable;
- receipt method and payload validation work without writing synthetic personal
  content;
- catalog rebuild dry-run matches current durable artifacts;
- static assets contain the expected workspace markers;
- post-deployment logs have no panic, fatal, migration, or pipeline errors.

**Step 4: Coordinate the health pilot**

Run consumer contract tests against the deployed feed, import a bounded approved
release into a candidate workspace, verify lineage and receipt, and keep domain
publication behind the consumer's existing review gate.

**Step 5: Record G5/G6 and commit the rollout evidence**

```bash
git add docs/dossiers/2026-07-14-knowledge-supply-platform.md
git commit -m "docs(kbase): record knowledge supply rollout"
```

Stop after Checkpoint 3 if the health consumer cannot preserve release/claim
identity or enforce `evidence_only`. Do not compensate by exposing raw KBase
search at consumer runtime.

