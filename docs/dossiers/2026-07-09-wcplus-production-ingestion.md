# WC Plus Production Ingestion Dossier

## Status

- **Current stage:** S4 - requirement decomposition
- **Delivery status:** ready for implementation
- **Architecture decision:** approved
- **Last updated:** 2026-07-09

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

## Gate Ledger

| Gate | Status | Evidence | Next action |
| --- | --- | --- | --- |
| G1 Admission | PASS | User confirmed local Agent plus online control plane | Preserve approved scope |
| G2 Feasibility/risk | PASS WITH CONSTRAINTS | Design documents auth, loopback, privacy, idempotency, and rollback | Start Task 1 |
| G3 Tests | PENDING | Full matrix not yet run for implementation | Complete Tasks 1-9 |
| G4 Review | PENDING | Security/data path requires independent review | Review after G3 |
| G5 Deploy health | PENDING | No new runtime deployed | Deploy from clean main after G4 |
| G6 Production validation | PENDING | No real local-to-online run yet | Execute staged validation with user |

## Pending Decisions

None block Task 1. Before scheduled synchronization is enabled, confirm the
initial subscription interval and maximum articles per run.

## Rollback

- Disable source-agent token configuration and scheduler leasing.
- Unload the local Agent while preserving its outbox.
- Keep existing imported knowledge and source-sync audit records.
- Continue using direct local proxy or manual import.

## Completion Record

Not shipped. Fill in commit IDs, artifact hashes, backup identifier, production
run IDs, outcome counters, and user confirmation only after the corresponding
gates pass.
