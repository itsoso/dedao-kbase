# WC Plus Production Ingestion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a production-safe path that lets a local WC Plus agent execute acquisition work and push idempotent, observable article updates into the online KBase control plane.

**Architecture:** Correct the WC Plus API contract first, then add a SQLite-backed source-sync state machine to KBase. A new `wcplus-agent` process accesses only the local WC Plus API and communicates with KBase over outbound HTTPS using a dedicated, limited token; the server normalizes, deduplicates, chunks, cites, and persists accepted articles.

**Tech Stack:** Go, `net/http`, `database/sql`, `github.com/mattn/go-sqlite3`, existing `BookKnowledgeStore`, existing static `frontend-web`, macOS LaunchAgent.

---

## Delivery Rules

- Preserve the existing direct WC Plus proxy and manual import during rollout.
- Do not expose the local WC Plus port or send WeChat/WC Plus cookies to KBase.
- Write a failing test before each behavior change.
- Commit each task independently and stage only the listed files.
- Do not deploy until the complete test matrix, privacy smoke, and independent
  review pass.
- Use sanitized contract fixtures only; never commit downloaded article bodies
  or local WC Plus databases.

### Task 1: Correct the WC Plus Task Contract

**Files:**
- Modify: `backend/app/wcplus_source.go`
- Modify: `backend/app/wcplus_source_test.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/wcplus-source-ui-smoke.mjs`

**Step 1: Write failing task-payload tests**

Add tests for all documented task families:

```go
func TestWCPlusSourceCreatesCompleteArticleTask(t *testing.T) {
    // Assert crawlerType=article, articleRefresh, and articleImgDownload.
}

func TestWCPlusSourceCreatesCompleteReadingDataTask(t *testing.T) {
    // Assert readingDataType, date/amount fields, only-main, and refresh.
}

func TestWCPlusSourceBatchNicknameUsesTaskNew(t *testing.T) {
    // Assert one /api/task/new call per exact nickname match.
}
```

Extend `WCPlusTaskRequest` with typed JSON fields:

```go
type WCPlusTaskRequest struct {
    Biz                    string `json:"biz"`
    Nickname               string `json:"nickname,omitempty"`
    ImageURL               string `json:"img,omitempty"`
    CrawlerType            string `json:"crawlerType,omitempty"`
    ArticleListType        string `json:"articleListType,omitempty"`
    ArticleListDate        int64  `json:"articleListDate,omitempty"`
    ArticleListAmount      int    `json:"articleListAmount,omitempty"`
    ArticleListOffset      int    `json:"articleListOffset,omitempty"`
    ArticleRefresh         bool   `json:"articleRefresh,omitempty"`
    ArticleImageDownload   bool   `json:"articleImgDownload,omitempty"`
    ReadingDataType        string `json:"readingDataType,omitempty"`
    ReadingDataStartDate   int64  `json:"readingDataStartDate,omitempty"`
    ReadingDataEndDate     int64  `json:"readingDataEndDate,omitempty"`
    ReadingDataAmount      int    `json:"readingDataAmount,omitempty"`
    ReadingDataOnlyMain    bool   `json:"readingDataOnlyMain,omitempty"`
    ReadingDataRefresh     bool   `json:"readingDataRefresh,omitempty"`
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./backend/app -run 'TestWCPlusSourceCreatesComplete|TestWCPlusSourceBatchNicknameUsesTaskNew' -count=1
```

Expected: FAIL because fields are missing and nickname import still calls the
global batch endpoint.

**Step 3: Implement the corrected contract**

- Send per-account tasks to `/api/task/new`.
- Keep `/api/batch_task/create_task` only for global batch-update configuration.
- Normalize upstream `status`, `msg`, `status_error`, progress counters, task
  type, and timestamps into `WCPlusTask`.
- Start the queue only with `{"command":"run"}`.
- Remove per-task start/stop controls from the Web UI until a sanitized live
  fixture proves that contract.
- Add task-family-specific fields to the diagnostics UI.

**Step 4: Add completion-disappearance regression tests**

Add a test where `/api/task/all` first returns a running task and then omits it.
Require a supplied article-state verifier to confirm completion:

```go
type WCPlusTaskVerifier func(context.Context, WCPlusBatchImportItem) (bool, error)
```

An omitted task without successful verification must return an explicit
`task outcome could not be verified` error, not success and not a generic
timeout.

**Step 5: Verify the corrected slice**

Run:

```bash
go test ./backend/app -run 'TestWCPlusSource|TestKBaseHTTPHandler.*WCPlus' -count=1
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
node --check frontend-web/app.js
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/wcplus_source.go backend/app/wcplus_source_test.go backend/app/kbase_http_test.go frontend-web/app.js frontend-web/scripts/wcplus-source-ui-smoke.mjs
git commit -m "fix(wcplus): align task workflows with local api"
```

### Task 2: Add the Source-Sync SQLite State Machine

**Files:**
- Create: `backend/app/source_sync.go`
- Create: `backend/app/source_sync_test.go`

**Step 1: Write failing migration and lifecycle tests**

Cover:

- schema migration on an empty KBase root
- agent heartbeat upsert
- subscription create/update/list
- queued run creation
- atomic lease acquisition
- wrong-agent completion rejection
- expired lease recovery
- terminal state immutability
- persisted new/updated/skipped/failed counters

Use fixed timestamps through an injected clock:

```go
type SourceSyncStore struct {
    dbPath string
    now    func() time.Time
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./backend/app -run TestSourceSyncStore -count=1
```

Expected: FAIL because `SourceSyncStore` does not exist.

**Step 3: Implement schema and core models**

Create tables and indexes for:

```sql
source_agents
source_subscriptions
source_sync_runs
source_sync_items
source_documents
source_outbox_receipts
```

Use `PRAGMA busy_timeout = 5000`, transactions for lease/state transitions,
and unique constraints for source identity and idempotency keys.

Define explicit constants:

```go
const (
    SourceRunQueued    = "queued"
    SourceRunLeased    = "leased"
    SourceRunRunning   = "running"
    SourceRunSucceeded = "succeeded"
    SourceRunPartial   = "partial"
    SourceRunFailed    = "failed"
    SourceRunCanceled  = "canceled"
)
```

**Step 4: Implement conditional transitions**

- `LeaseNextRun(agentID, capabilities, leaseDuration)` changes exactly one
  eligible row from `queued` to `leased`.
- `StartRun` requires the active lease owner.
- `CompleteRun` computes terminal state from persisted item outcomes.
- `RequeueExpiredRuns` requeues only nonterminal expired leases.
- `RetryRun` creates a new run linked to the failed/partial run.
- `CancelRun` does not alter already terminal runs.

**Step 5: Verify**

Run:

```bash
go test ./backend/app -run TestSourceSyncStore -count=1
```

Expected: PASS, including closing and reopening the store between lifecycle
steps to prove restart recovery.

**Step 6: Commit**

```bash
git add backend/app/source_sync.go backend/app/source_sync_test.go
git commit -m "feat(kbase): persist source sync state"
```

### Task 3: Add Dedicated Source-Agent HTTP Authentication

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

**Step 1: Write failing authentication tests**

Test that:

- existing `KBASE_AUTH_TOKEN` cannot call source-agent write routes
- `KBASE_SOURCE_AGENT_TOKEN` can call only `/api/source-agent/*`
- the source-agent token cannot call browser/admin routes
- empty source-agent configuration returns `503` for agent routes
- invalid tokens return `401` without echoing the token
- article payloads larger than the configured limit return `413`

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./backend/app ./cmd/kbase-server -run 'Test.*SourceAgent' -count=1
```

Expected: FAIL with route-not-found assertions.

**Step 3: Add configuration and route groups**

Extend `KBaseHTTPConfig`:

```go
SourceSync       *SourceSyncStore
SourceAgentToken string
```

Read `KBASE_SOURCE_AGENT_TOKEN` in `cmd/kbase-server`. Use constant-time token
comparison and route agent requests before the existing admin-token middleware,
but never allow an agent route to fall through into admin routing.

Implement:

- `POST /api/source-agent/heartbeat`
- `POST /api/source-agent/lease`
- `POST /api/source-agent/runs/{run_id}/items`
- `POST /api/source-agent/runs/{run_id}/complete`
- `POST /api/source-agent/runs/{run_id}/fail`
- `GET /api/source-agents`
- `GET|POST /api/source-subscriptions`
- `POST /api/source-subscriptions/{id}/sync`
- `GET /api/source-sync/runs`
- `GET /api/source-sync/runs/{id}`
- `POST /api/source-sync/runs/{id}/retry`
- `POST /api/source-sync/runs/{id}/cancel`

**Step 4: Verify**

Run:

```bash
go test ./backend/app ./cmd/kbase-server -run 'Test.*SourceAgent|Test.*SourceSyncHTTP' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go cmd/kbase-server/main_test.go
git commit -m "feat(kbase): add source agent control api"
```

### Task 4: Implement Idempotent Article Normalization

**Files:**
- Create: `backend/app/source_ingest.go`
- Create: `backend/app/source_ingest_test.go`
- Modify: `backend/app/book_knowledge.go`
- Modify: `backend/app/book_knowledge_test.go`
- Modify: `backend/app/kbase_http.go`

**Step 1: Write failing ingestion tests**

Define the input contract:

```go
type SourceArticleEnvelope struct {
    IdempotencyKey  string            `json:"idempotency_key"`
    SourceType      string            `json:"source_type"`
    SourceAccountID string            `json:"source_account_key"`
    SourceAccount   string            `json:"source_account"`
    SourceItemID    string            `json:"source_item_key"`
    Title           string            `json:"title"`
    Author          string            `json:"author,omitempty"`
    SourceURL       string            `json:"source_url"`
    PublishedAt     string            `json:"published_at,omitempty"`
    Content         string            `json:"content"`
    ContentFormat   string            `json:"content_format"`
    Metadata        map[string]string `json:"metadata,omitempty"`
}
```

Cover:

- first import returns `new`
- unchanged reimport returns `skipped`
- changed content returns `updated` and keeps original `CreatedAt`
- repeated idempotency key returns the same receipt
- large Markdown creates multiple bounded chunks
- each chunk has a citation
- empty/implausibly short content is rejected without a book manifest
- imported books remain discoverable through current search/export APIs

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./backend/app -run 'TestIngestSourceArticle|TestBookKnowledgeSourceMetadata' -count=1
```

Expected: FAIL because the ingestion service and metadata fields are absent.

**Step 3: Add backward-compatible source metadata**

Add optional `omitempty` fields to `BookKnowledgeBook`:

```go
SourceType    string `json:"source_type,omitempty"`
SourceKey     string `json:"source_key,omitempty"`
SourceAccount string `json:"source_account,omitempty"`
PublishedAt   string `json:"published_at,omitempty"`
ContentHash   string `json:"content_hash,omitempty"`
```

Do not change existing required fields or package version.

**Step 4: Implement normalization and persistence**

- Canonicalize and validate the source URL.
- Compute SHA-256 server-side from normalized content.
- Derive a deterministic book ID from source type and source item key.
- Reuse the existing knowledge text splitter for bounded chunks.
- Attach source URL, publication time, account, and source item key to every
  citation.
- Write the book package before committing the source document and item receipt;
  if the file write fails, keep the item failed and do not advance the run.

**Step 5: Verify**

Run:

```bash
go test ./backend/app -run 'TestIngestSourceArticle|TestBookKnowledge' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_ingest.go backend/app/source_ingest_test.go backend/app/book_knowledge.go backend/app/book_knowledge_test.go backend/app/kbase_http.go
git commit -m "feat(kbase): ingest idempotent source articles"
```

### Task 5: Build the Local Agent Client and Durable Outbox

**Files:**
- Create: `backend/app/source_agent_client.go`
- Create: `backend/app/source_agent_client_test.go`
- Create: `backend/app/source_agent_outbox.go`
- Create: `backend/app/source_agent_outbox_test.go`
- Create: `cmd/wcplus-agent/main.go`
- Create: `cmd/wcplus-agent/main_test.go`

**Step 1: Write failing configuration tests**

Require explicit values for remote URL, agent token, agent ID, and state
directory. Allow the documented WC Plus localhost default, but reject a remote
URL without HTTPS unless the hostname is loopback for tests/development.

**Step 2: Write failing heartbeat and lease tests**

Use an `httptest.Server` to assert:

- `Authorization: Bearer` contains only the source-agent token
- heartbeat contains agent version, supported task types, and local WC Plus
  health
- lease requests identify the agent and supported capabilities
- `401`, `409`, and `503` remain visible to the caller
- response bodies never enter logs

**Step 3: Write failing outbox tests**

Cover enqueue, ordered peek, acknowledge, retry count, next-at scheduling,
process restart, and dead-letter transition. The outbox stores normalized
article envelopes only.

**Step 4: Implement the client and outbox**

Use a local SQLite file under `WCPLUS_AGENT_STATE_DIR`. Add bounded exponential
backoff with jitter and an explicit maximum attempt count. Keep terminal server
validation errors in dead letter; retry transport and `5xx` failures.

**Step 5: Add CLI modes**

Support:

```text
wcplus-agent doctor
wcplus-agent once
wcplus-agent run
```

- `doctor` checks local WC Plus and remote KBase authentication without writing.
- `once` performs one heartbeat, lease, execution, and outbox-flush cycle.
- `run` repeats cycles with context cancellation and a bounded poll interval.

**Step 6: Verify**

Run:

```bash
go test ./backend/app ./cmd/wcplus-agent -run 'TestSourceAgent|TestWCPlusAgent' -count=1
go build ./cmd/wcplus-agent
```

Expected: PASS and a successful build.

**Step 7: Commit**

```bash
git add backend/app/source_agent_client.go backend/app/source_agent_client_test.go backend/app/source_agent_outbox.go backend/app/source_agent_outbox_test.go cmd/wcplus-agent/main.go cmd/wcplus-agent/main_test.go
git commit -m "feat(wcplus): add local source agent"
```

### Task 6: Execute Leased WC Plus Runs End to End

**Files:**
- Create: `backend/app/wcplus_agent.go`
- Create: `backend/app/wcplus_agent_test.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `backend/app/wcplus_source.go`

**Step 1: Write a failing end-to-end agent test**

Use one fake local WC Plus server and one fake KBase server. Queue an account
sync run and require this sequence:

```text
heartbeat -> lease -> local account/articles -> local content
          -> remote item upload -> remote complete
```

Assert the agent never sends local WC Plus configuration or request parameters
to the remote server.

**Step 2: Add task-backed synchronization tests**

Cover:

- missing link data creates `/api/task/new` with `gzh_article_link`
- queue starts with `command=run`
- running task progress is reported
- task disappearance plus verified article availability succeeds
- task disappearance without article verification fails explicitly
- `not_max_version`, `unactivated`, throttling, and parameter expiry stop without
  automatic retry
- one bad article yields a partial run while other articles continue

**Step 3: Implement run execution**

Interpret run options for:

- `existing_articles`: import already available content only
- `sync_links`: refresh article links, then import
- `sync_content`: ensure links and bodies, then import
- `sync_reading_data`: collect metrics without placing sensitive request data in
  KBase

Advance a subscription cursor only after accepted items and terminal run state
have been persisted.

**Step 4: Verify**

Run:

```bash
go test ./backend/app ./cmd/wcplus-agent -run 'TestWCPlusAgent|TestWCPlusSource' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/wcplus_agent.go backend/app/wcplus_agent_test.go backend/app/wcplus_source.go cmd/wcplus-agent/main.go
git commit -m "feat(wcplus): execute leased source sync runs"
```

### Task 7: Add Subscription Scheduling

**Files:**
- Create: `backend/app/source_scheduler.go`
- Create: `backend/app/source_scheduler_test.go`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

**Step 1: Write failing scheduler tests**

Cover due, disabled, already-active, future, and failed-retry subscriptions.
Inject time and call one scheduler tick directly; tests must not sleep.

**Step 2: Implement conservative scheduling**

- Store simple interval schedules in seconds for the first version.
- Enqueue at most one active run per subscription.
- Run scheduling only when source-agent support is configured.
- Keep the ticker lifecycle tied to server context and log counts, not content.
- Do not auto-retry blocked authorization/license/parameter failures.

**Step 3: Verify**

Run:

```bash
go test ./backend/app ./cmd/kbase-server -run 'TestSourceScheduler|Test.*SourceAgent' -count=1
```

Expected: PASS.

**Step 4: Commit**

```bash
git add backend/app/source_scheduler.go backend/app/source_scheduler_test.go cmd/kbase-server/main.go cmd/kbase-server/main_test.go
git commit -m "feat(kbase): schedule source subscriptions"
```

### Task 8: Build the Web Control Plane

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`
- Create: `frontend-web/scripts/wcplus-control-plane-smoke.mjs`
- Modify: `frontend-web/scripts/wcplus-source-ui-smoke.mjs`

**Step 1: Write failing smoke assertions**

Require UI and API markers for:

- agent online/offline and last heartbeat
- WC Plus capability health/version
- subscription create, enable/disable, and sync-now
- queued/running/partial/failed/succeeded run filters
- run counters and item errors
- retry and cancel
- direct REST knowledge links
- collapsible legacy diagnostics

**Step 2: Run smoke and verify failure**

Run:

```bash
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
```

Expected: FAIL because the control-plane UI is absent.

**Step 3: Implement the durable UI**

Use the existing wide two-column application shell:

- left: agent and subscription list
- center: selected subscription and run history
- optional drawer: run items, errors, and imported knowledge links

Do not keep authoritative run state in page-only memory. Reload state from the
control-plane APIs after every mutation and on a bounded poll while a run is
active. Keep direct WC Plus proxy tools collapsed under diagnostics.

**Step 4: Verify**

Run:

```bash
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/kbase-token-header-smoke.mjs
node --check frontend-web/app.js
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html frontend-web/scripts/wcplus-control-plane-smoke.mjs frontend-web/scripts/wcplus-source-ui-smoke.mjs
git commit -m "feat(web): add wcplus source control plane"
```

### Task 9: Package the macOS Agent

**Files:**
- Create: `scripts/build-wcplus-agent-macos.sh`
- Create: `scripts/install-wcplus-agent-macos.sh`
- Create: `scripts/uninstall-wcplus-agent-macos.sh`
- Modify: `README.md`

**Step 1: Write script validation checks**

Add shell self-check modes that fail when required environment variables are
missing and print variable names only, never values.

**Step 2: Implement build and installation**

- Build the native agent binary for the current macOS architecture.
- Generate a LaunchAgent plist at install time from explicit environment values.
- Keep the agent state directory and log directory configurable.
- Use `KeepAlive` only for unexpected exits and a bounded restart interval.
- Provide unload/removal without deleting the outbox unless explicitly asked.

**Step 3: Document configuration with placeholders**

Document only variable contracts:

```bash
KBASE_REMOTE_URL="https://kbase.example.invalid"
KBASE_SOURCE_AGENT_ID="wcplus-agent-1"
KBASE_SOURCE_AGENT_TOKEN="replace-with-source-agent-secret"
WCPLUSPRO_BASE_URL="http://127.0.0.1:5001"
WCPLUS_AGENT_STATE_DIR="./state/wcplus-agent"
```

Do not include machine-specific paths, real hosts, tokens, or downloaded data.

**Step 4: Verify**

Run:

```bash
bash -n scripts/build-wcplus-agent-macos.sh
bash -n scripts/install-wcplus-agent-macos.sh
bash -n scripts/uninstall-wcplus-agent-macos.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: PASS.

**Step 5: Commit**

```bash
git add scripts/build-wcplus-agent-macos.sh scripts/install-wcplus-agent-macos.sh scripts/uninstall-wcplus-agent-macos.sh README.md
git commit -m "build(wcplus): package local source agent"
```

### Task 10: Full Verification, Review, and Production Rollout

**Files:**
- Modify: `docs/dossiers/2026-07-09-wcplus-production-ingestion.md`
- Modify other files only if a gate exposes a defect.

**Step 1: Run the complete local gate**

Run without piping through plain `tail`:

```bash
go test ./... -count=1
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/kbase-token-header-smoke.mjs
node --check frontend-web/app.js
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS.

**Step 2: Perform independent review**

Review authentication separation, lease transitions, SQLite transactions,
idempotency, request limits, log redaction, and rollback behavior. Record G4 in
the dossier. Any finding blocks deployment until fixed and re-reviewed.

**Step 3: Build release artifacts**

Build the online server for its deployment platform and the native macOS agent.
Record artifact hashes and the pre-deploy server backup identifier in the
dossier.

**Step 4: Deploy server capability disabled by default**

Deploy the server/frontend first. Verify:

- `/health` succeeds
- existing `/api/books`, search, reader, and WC Plus diagnostics still work
- source-agent routes return `503` until the dedicated token is configured

If any existing path regresses, roll back immediately.

**Step 5: Enable heartbeat only**

Configure the dedicated token on both sides, install the local agent, and run
`wcplus-agent doctor` followed by `wcplus-agent once`. Verify the Web control
plane shows the agent and local WC Plus capability status without receiving any
sensitive acquisition state.

**Step 6: Execute staged real-path validation**

In order:

1. one manually selected existing article
2. unchanged replay, expected `skipped`
3. one bounded account synchronization
4. agent restart during a run, expected lease recovery
5. temporary online outage, expected outbox replay
6. one scheduled subscription

Record run IDs, counters, target book IDs, and visible failure reasons in the
dossier. Do not record article bodies or credentials.

**Step 7: Complete the lifecycle record**

Mark G3-G6 only from observed evidence. Set dossier status to `shipped` only
after user confirmation of the production Web control plane and imported
knowledge documents.

**Step 8: Commit rollout records**

```bash
git add docs/dossiers/2026-07-09-wcplus-production-ingestion.md
git commit -m "docs(wcplus): record production ingestion rollout"
```

## Execution Order and Checkpoints

- Checkpoint A after Task 1: confirm real WC Plus contract compatibility.
- Checkpoint B after Task 4: confirm server state, auth, and idempotent ingestion
  before building the agent.
- Checkpoint C after Task 6: confirm one local-to-online end-to-end sync.
- Checkpoint D after Task 8: review product workflow before packaging.
- Checkpoint E at Task 10 G5/G6: user-visible production verification.

Do not skip a checkpoint or continue with a red gate.
