# WC Plus Production Ingestion Dossier

## Status

- **Current stage:** S7 - verification and rollout
- **Delivery status:** Tasks 1-9 complete; Checkpoints A-D pass; Task 10 in progress
- **Architecture decision:** approved
- **Last updated:** 2026-07-10

## S0 Intake

User requests:

> WC Plus相关的功能 还有哪些没有完成的 形成新规划

> 采用 本地 Agent + 在线 KBase 控制面

Primary user: the operator who collects public-account content on a local
computer and needs it available as durable online knowledge.

Problem: WC Plus is local, while KBase is online. Existing proxy and import
features cover many APIs but do not provide a secure, restart-safe, automated
local-to-online synchronization path.

Current workaround: manually import text/files or run KBase where it can reach
the local WC Plus service.

## S1 Current-State Findings

Reusable capabilities:

- WC Plus account/article listing, pagination, search, content preview, and
  exports.
- Single, account, nickname-batch, and raw article import.
- Existing Bearer-protected KBase HTTP server and online Web workbench.
- Existing `BookKnowledgeStore` and SQLite pattern.
- Existing REST-style links to books and knowledge results.

Confirmed gaps:

- The online service cannot reach a local loopback WC Plus endpoint.
- Current task models omit documented task-family fields.
- Nickname batch import uses the wrong upstream task endpoint semantics.
- Completion polling can misclassify a task that disappears from the
  unfinished-task list.
- Sync runs, items, retries, leases, and recent imports are not durably modeled.
- Reimports do not report accurate new/updated/skipped outcomes.
- WC Plus articles are written as a single unbounded chunk.
- No local agent, durable outbox, subscription scheduler, or Agent status UI.

Hard constraints:

- Never expose the unauthenticated WC Plus local API publicly.
- Never send WeChat cookies or WC Plus request-parameter records online.
- Preserve existing direct proxy and manual import during migration.
- Preserve unrelated dirty-worktree changes and stage only feature files.

## G1 Admission

**Decision:** PASS

Reason: secure and automated source ingestion directly supports the repository's
role as a private knowledge workbench. The smallest end-to-end slice is one
local agent heartbeat followed by one idempotent article upload.

## S2 Product Definition

The feature provides:

- a local outbound-only WC Plus agent
- an online durable source control plane
- idempotent article ingestion and knowledge normalization
- observable agents, subscriptions, runs, items, and failures
- conservative scheduling and explicit retries

It does not reproduce WC Plus licensing, proxy/certificate management, payment,
AI prompts, or other administrative functions.

Design:

- [`../plans/2026-07-09-wcplus-production-ingestion-design.md`](../plans/2026-07-09-wcplus-production-ingestion-design.md)

## G2 Feasibility and Risk Review

**Decision:** PASS WITH HARD CONSTRAINTS

Approved approach: local Agent plus online KBase control plane.

Key risks and controls:

- Unauthenticated local API: loopback only; outbound agent transport.
- Token scope: separate source-agent token and route group.
- Duplicate delivery: server-computed content hash and idempotency receipt.
- Agent/server restart: SQLite outbox, leases, and conditional transitions.
- Upstream contract drift: sanitized real fixtures and capability heartbeat.
- Partial content: validation, partial run state, and item-level errors.
- Privacy leakage: normalized content only, redacted logs, privacy smoke.

## S3 Plan

Implementation plan:

- [`../plans/2026-07-09-wcplus-production-ingestion.md`](../plans/2026-07-09-wcplus-production-ingestion.md)

Delivery phases:

1. Correct WC Plus task contracts.
2. Persist source agents, subscriptions, runs, items, documents, and receipts.
3. Add dedicated source-agent HTTP authentication and APIs.
4. Normalize and idempotently persist source articles.
5. Build local client and durable outbox.
6. Execute leased WC Plus runs end to end.
7. Add scheduling.
8. Add the Web control plane.
9. Package the macOS Agent.
10. Pass full verification, review, deployment, and production validation.

## S4 Requirement Decomposition

The task-level file changes, failing tests, verification commands, and commit
boundaries are defined in the implementation plan. Implementation must stop at
checkpoints A-E when evidence is missing or a gate is red.

## S5 Implementation Progress

Completed commits:

- `c3ebac5` corrects per-account task creation, task normalization, completion
  verification, and the Web diagnostics contract.
- `be49404` adds the SQLite source agents, subscriptions, runs, items,
  documents, receipts, leases, retries, and terminal-state model.
- `93d64c3` separates source-agent authentication from browser/admin
  authentication and adds the source control APIs and request limits.
- `4a7ac3d` adds server-computed content hashes, idempotency receipts,
  new/updated/skipped outcomes, bounded chunks, and per-chunk provenance.
- `fa37893` accepts the current WC Plus account-list response shape.
- `4684138` adds the outbound-only local Agent client and durable SQLite
  outbox.
- `5a7b02c` executes leased article and reading-data runs end to end.
- `ad41258` adds conservative subscription scheduling and lease recovery.
- `c627941` adds the wide Web source control plane with agent,
  subscription, run, retry, and REST knowledge-document workflows.
- `fd2f7f5` packages the native macOS Agent as a bounded user LaunchAgent.
- `7f40949` adds atomic knowledge writes, concurrent-ingestion protection,
  diagnostic limits, and source/admin token separation checks.
- `cf1efa4` aligns article-list defaults with WC Plus 9.483 and reports the
  observed WC Plus version through Agent heartbeats.

Checkpoint evidence:

- **Checkpoint A: PASS.** A local WC Plus 9.483 instance was observed on its
  displayed loopback port (`5002`). Account, article-list, and article-content
  calls succeeded. The live article-list contract requires `sort=p_date` and
  `direction=desc`; both are now defaulted and covered by regression tests.
  Only response field shapes were inspected; no account name, article title,
  body, cookie, or request-parameter record was retained.
- **Checkpoint B: PASS for the server slice.** Source state, token isolation,
  request limits, restart persistence, idempotent replay, update preservation,
  failed-write behavior, search discovery, and chunk citations are covered by
  passing tests.
- **Checkpoint C: PASS for the isolated end-to-end path.** One real local
  existing article was processed through `wcplus-agent once` into an isolated
  KBase control plane. The run finished `succeeded` with `uploaded=1`,
  `failed=0`, `outbox_remaining=0`, and server counters `new=1`, `updated=0`,
  `skipped=0`, `failed=0`. The resulting private knowledge document contained
  one chapter, one bounded chunk, one citation, and complete provenance. All
  temporary article and knowledge data was deleted after validation.
- **Checkpoint D: PASS.** Desktop and mobile browser checks covered agent
  health/version, subscriptions, runs, item failures, retry/cancel actions,
  detail drawers, and REST-style knowledge links. Legacy direct-proxy tools
  remain collapsed under diagnostics.

Verification executed through Task 9 and the live-contract correction:

```text
go test ./... -count=1
go vet ./...
go test -race ./backend/app ./cmd/kbase-server ./cmd/wcplus-agent -count=1
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/kbase-token-header-smoke.mjs
node --check frontend-web/app.js
bash scripts/wcplus-agent-packaging-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

The fresh final Task 10 matrix passed before release artifact creation. The
Vue build emitted its existing eval and large-chunk warnings but returned zero;
the race run emitted macOS linker warnings but all three tested packages
returned `ok`.

## Release Artifacts

- Source commit: `cf1efa4d2dd58f7ff09d12112d5e0b4582d907f1`
- Clean source archive SHA-256:
  `f94f0844091919682113fc9f620cff7b5bb4fc4f5d5ae430dbc340887d3052d0`
- Native macOS `arm64` Agent SHA-256:
  `f17dd26ddb174734ee5c1b747253107a0a6c6d9c705c39da8dadbb08d00ae1cd`
- Linux `x86-64` server SHA-256:
  `c9064e9faebe0460699fe88d8cfc96f0ff3c0eaba627b067f456b90d5a6fcde7`
- Linux build used Go 1.23.0 after checking the toolchain archive against
  SHA-256 published in the official Go release metadata. The initial module
  download through `proxy.golang.org` timed out; the successful retry used an
  HTTPS Go module mirror without disabling `go.sum` verification.
- Pre-deploy backup identifier: pending G5.

## G4 Independent Review

**Decision:** PASS

The review covered dedicated-token route isolation, lease ownership and expiry,
SQLite transactions, idempotency receipts, upload/request limits, diagnostic
redaction and truncation, crash-safe outbox replay, atomic knowledge manifests,
and rollback behavior. Findings were fixed in `7f40949`; the race detector then
passed for the ingestion server and Agent packages.

## Gate Ledger

| Gate | Status | Evidence | Next action |
| --- | --- | --- | --- |
| G1 Admission | PASS | User confirmed local Agent plus online control plane | Preserve approved scope |
| G2 Feasibility/risk | PASS WITH CONSTRAINTS | Design documents auth, loopback, privacy, idempotency, and rollback | Start Task 1 |
| G3 Tests | PASS | Fresh Go tests/vet/race, Vue build, Web smokes, packaging, privacy, and diff checks passed | Preserve tested commit and artifact hashes |
| G4 Review | PASS | Auth, leases, transactions, idempotency, limits, redaction, outbox, atomic writes, and rollback reviewed; findings fixed in `7f40949` | Preserve release diff |
| G5 Deploy health | PENDING | No new runtime deployed | Deploy from clean main after G4 |
| G6 Production validation | PENDING | No production local-to-online run yet | Execute staged validation with user |

## Pending Decisions

No design decision blocks Task 10. Production source-agent authentication and
the first bounded subscription remain disabled until the capability-only
deployment passes existing-path checks.

## Rollback

- Disable source-agent token configuration and scheduler leasing.
- Unload the local Agent while preserving its outbox.
- Keep existing imported knowledge and source-sync audit records.
- Continue using direct local proxy or manual import.

## Completion Record

Not shipped. Tasks 1-9 and Checkpoints A-D are complete on the feature branch.
Release artifact hashes, the pre-deploy backup identifier, production run IDs,
outcome counters, and user confirmation are recorded only after G5/G6 produce
direct evidence.
