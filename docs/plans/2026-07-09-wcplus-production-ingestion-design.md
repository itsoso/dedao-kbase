# WC Plus Production Ingestion Design

**Status:** Approved

**Decision:** Use a local WC Plus agent with an online KBase control plane.

## Context

The current integration covers WC Plus account and article browsing, search,
preview, export, task controls, and direct import into book knowledge. It is not
yet a production ingestion path because the online KBase process cannot reach a
WC Plus service bound to a personal computer's `127.0.0.1:5001`.

The existing task adapter also models only a subset of the documented WC Plus
task fields. Its batch nickname workflow calls the global batch-task endpoint
for each account, while the documented skill workflow creates one
`gzh_article_link` task per account through `/api/task/new`. Completion polling
also relies on `/api/task/all`, even though completed tasks may disappear from
that unfinished-task list.

## Goals

- Keep WC Plus and all WeChat-side acquisition state on the local computer.
- Let the online KBase request and observe synchronization without opening the
  WC Plus port to the network.
- Persist subscriptions, runs, item outcomes, leases, retries, and audit data.
- Make repeated synchronization idempotent and report new, updated, skipped,
  and failed item counts accurately.
- Normalize imported articles into well-chunked, citation-ready knowledge
  packages for downstream consumers.
- Preserve the current manual import and direct local proxy workflows as
  fallbacks during migration.

## Non-Goals

- Reimplement WC Plus licensing, payment, CA certificate, proxy, activation, or
  account-parameter management.
- Expose the unauthenticated WC Plus local API through a public tunnel.
- Copy WC Plus prompt, AI-task, category, favorite, or administrative screens.
- Store WeChat cookies, WC Plus request parameters, or downloaded raw databases
  in the online control plane.
- Replace WC Plus as the acquisition engine in this phase.

## Options Considered

### Public or reverse-tunneled WC Plus API

This is the smallest code change, but it publishes a local API that has no
native authentication and exposes more operations than KBase needs. It also
couples production availability to a long-lived tunnel. Rejected.

### Run WC Plus on the online server

This avoids a bridge but does not fit the desktop WeChat, local proxy, and
certificate workflow. It also moves sensitive acquisition state to the server.
Rejected.

### Local outbound agent plus online control plane

The agent can access local WC Plus and initiate only outbound HTTPS requests.
KBase retains durable scheduling, run history, idempotency, and knowledge
normalization. Selected.

## Architecture

```text
WC Plus localhost API
  -> wcplus-agent
     -> local health and contract checks
     -> command lease and task execution
     -> durable local outbox
     -> outbound authenticated HTTPS
        -> KBase source-agent API
           -> source sync SQLite store
           -> idempotent article normalizer
           -> BookKnowledgeStore
           -> downstream evidence export surfaces
```

### Local agent

Add `cmd/wcplus-agent` as a small Go process. It uses the existing
`WCPlusSourceService` and reads configuration only from flags or environment:

- `WCPLUSPRO_BASE_URL` or `WCPLUS_BASE_URL`
- `KBASE_REMOTE_URL`
- `KBASE_SOURCE_AGENT_TOKEN`
- `KBASE_SOURCE_AGENT_ID`
- `WCPLUS_AGENT_STATE_DIR`

The agent sends heartbeats, leases queued runs, executes WC Plus operations,
uploads normalized article envelopes, and reports terminal run state. A local
SQLite outbox keeps unsent envelopes when KBase is unavailable. It never stores
WeChat cookies or WC Plus request-parameter records.

### Online control plane

Add a source-sync store under the configured KBase root. Reuse the repository's
SQLite driver and migration pattern. The initial schema contains:

- `source_agents`: agent identity, version, capabilities, last heartbeat, and
  last error.
- `source_subscriptions`: source account, selected agent, schedule, cursor,
  options, and enabled state.
- `source_sync_runs`: status, lease, requested operation, counters, timestamps,
  and terminal error.
- `source_sync_items`: source item identity, content hash, outcome, target book,
  and item error.
- `source_documents`: stable source identity, current content hash, target book,
  source timestamp, and last-seen timestamp.
- `source_outbox_receipts`: idempotency key and accepted-at timestamp for replay
  protection.

Run states are:

```text
queued -> leased -> running -> succeeded
                         |----> partial
                         |----> failed
queued/leased/running --------> canceled
expired lease ----------------> queued
```

State transitions use conditional SQL updates. An agent cannot complete a run
it does not currently lease. Expired leases return to `queued`; they are never
silently marked successful.

### API boundary

Agent routes use `KBASE_SOURCE_AGENT_TOKEN`, which is separate from the browser
and automation `KBASE_AUTH_TOKEN`:

- `POST /api/source-agent/heartbeat`
- `POST /api/source-agent/lease`
- `POST /api/source-agent/runs/{run_id}/items`
- `POST /api/source-agent/runs/{run_id}/complete`
- `POST /api/source-agent/runs/{run_id}/fail`

Administrative routes keep the existing KBase Bearer authentication:

- `GET /api/source-agents`
- `GET|POST /api/source-subscriptions`
- `POST /api/source-subscriptions/{id}/sync`
- `GET /api/source-sync/runs`
- `GET /api/source-sync/runs/{id}`
- `POST /api/source-sync/runs/{id}/retry`
- `POST /api/source-sync/runs/{id}/cancel`

The browser never receives the source-agent token. Agent routes accept only the
minimum article and run payloads; they do not proxy arbitrary WC Plus paths.

## WC Plus Contract Corrections

- Create per-account link, article, and reading-data tasks through
  `/api/task/new` with complete typed fields.
- Reserve `/api/batch_task/create_task` for its documented global batch-update
  semantics.
- Start the queue with `{"command":"run"}`. Remove unsupported per-task
  start/stop actions unless a live contract fixture proves them.
- Treat disappearance from `/api/task/all` as an ambiguous signal. Confirm
  success from task status/list endpoints when available and from the expected
  article-list/content state before completing a run.
- Preserve upstream states such as `not_max_version`, `unactivated`, request
  throttling, and parameter expiry as explicit blocked or failed outcomes.
- Record the observed WC Plus version and capabilities in each agent heartbeat
  so incompatible operations can be disabled before execution.

## Ingestion Contract

The agent uploads a `SourceArticleEnvelope` containing:

- `source_type`, fixed to `wcplus_wechat_article`
- `source_account_key` and display name
- stable `source_item_key`
- title, author, canonical source URL, and publication time
- Markdown content and optional non-sensitive metadata
- an agent-generated idempotency key

The server validates and recomputes the content hash. The unique identity is
`(source_type, source_item_key)`. Outcomes are:

- `new`: no prior source document exists.
- `updated`: the content hash changed.
- `skipped`: the content hash is unchanged.
- `failed`: validation, normalization, or persistence failed.

Replaying the same idempotency key returns the original receipt without writing
the knowledge package again.

## Knowledge Normalization

WC Plus articles remain individual knowledge documents mapped onto the existing
book package format. Extend book metadata with optional source fields while
keeping existing manifests backward compatible.

- Use a deterministic book ID derived from the canonical source identity.
- Preserve `CreatedAt` across updates; update `UpdatedAt` only when content
  changes.
- Split Markdown by headings and the existing knowledge chunk-size rules rather
  than writing one unbounded chunk.
- Create one citation per chunk with source URL, publication time, account, and
  source item key.
- Reject empty or implausibly short bodies and flag partial extraction instead
  of publishing them as ready knowledge.
- Keep claims empty until the existing claim-extraction pipeline succeeds; do
  not fabricate claims during source ingestion.

## Scheduling and Automation

Subscriptions belong to the online control plane, but execution remains local.
The first scheduler creates due runs; an online agent leases them during its
poll cycle. Each subscription stores a cursor based on source publication time
and stable item key. The cursor advances only after a successful or explicitly
partial run has persisted all accepted items.

Default automation is conservative:

- one active run per subscription
- bounded page and item limits
- exponential retry for transport failures
- no automatic retry for authorization, license, parameter-expiry, or content
  validation failures
- manual retry from the Web control plane

## Web Experience

Keep `/wcplus-source` as the source workbench, but make durable control-plane
state the primary view:

- agent online/offline and capability status
- subscriptions and last successful sync
- queued, running, partial, failed, and completed runs
- per-run new/updated/skipped/failed counts
- explicit retry/cancel actions
- direct links to imported knowledge documents

The existing direct proxy tools remain in a collapsible diagnostics section for
local troubleshooting. Browser refresh must not lose run history.

## Failure Handling

- Local WC Plus unavailable: heartbeat stays online but reports the WC Plus
  capability unhealthy; no run is leased.
- Online KBase unavailable: envelopes remain in the local outbox and retry with
  backoff.
- Agent exits mid-run: lease expires and the run becomes queueable again.
- Upstream task disappears: verify article state before deciding success;
  otherwise mark the run `partial` or `failed` with a visible reason.
- Individual article failure: continue the batch, record the item error, and
  finish the run as `partial`.
- Duplicate payload: return the existing receipt and count it as `skipped`.
- Oversized or malformed payload: reject before writing files and keep the
  failure attached to the run.

## Security and Privacy

- Never expose port 5001 beyond loopback.
- Use a dedicated source-agent token with constant-time comparison and limited
  routes.
- Do not log authorization headers, cookies, raw request parameters, or full
  article bodies.
- Apply request-size limits and validate all source URLs and identifiers.
- Store only normalized article content and non-sensitive provenance online.
- Keep agent state and downloaded content outside the repository through an
  explicit state-directory setting.

## Testing Strategy

- Contract fixtures captured from sanitized real WC Plus responses.
- Unit tests for task payloads, response aliases, completion-by-disappearance,
  blocked upstream states, and version capabilities.
- SQLite state-machine tests for leases, retries, idempotency, and restart
  recovery.
- Agent tests with fake local WC Plus and fake online KBase servers.
- HTTP authentication and payload-limit tests for both token classes.
- Browser smoke plus functional DOM tests for durable run history.
- Optional live contract smoke guarded by explicit environment confirmation;
  it must never run implicitly in CI.

## Rollout

1. Ship server schema and agent endpoints disabled unless
   `KBASE_SOURCE_AGENT_TOKEN` is configured.
2. Install the local agent and verify heartbeat only.
3. Run one manual single-article synchronization.
4. Run one bounded account synchronization and validate idempotent replay.
5. Enable a single scheduled subscription.
6. Expand subscriptions only after partial failures, retries, and restart
   recovery are verified.

The legacy direct proxy and manual import remain available throughout rollout.
Rollback disables agent leasing and scheduling without deleting imported
knowledge or source-sync history.

## Acceptance Criteria

- The online server never connects directly to a personal localhost WC Plus
  service.
- A local agent can heartbeat, lease a run, upload an article, and complete the
  run over outbound HTTPS.
- Restarting either process does not lose accepted items or leave a run falsely
  successful.
- Reimporting unchanged content produces `skipped`; changed content produces
  `updated`; neither creates a duplicate book.
- Task creation and completion behavior match sanitized real WC Plus fixtures.
- Imported articles contain bounded chunks and chunk-level citations.
- The Web UI displays durable agent, subscription, run, item, and knowledge-link
  state after a full browser refresh.
- Tokens, cookies, machine-specific paths, and downloaded source samples do not
  enter git or application logs.
