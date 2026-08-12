# WeChat Account Collection Agent Dossier

## Status

- **Current stage:** S3 — implementation planning
- **Delivery status:** approved design; implementation not started
- **Last updated:** 2026-08-12

## S0 Intake

User requests:

> 把益家知研这个公众号 下载所有文章，做成知识库Agent

> 要的，并且 增加“公众号集合知识库 Agent”能力

Primary user: the operator who wants one cited, searchable Agent grounded in the
complete history currently visible for a selected WeChat public account.

Current workaround: individual articles can be imported and searched, and a
single book can become an Agent, but a large account cannot be represented as
one immutable Agent scope without manually handling many article artifacts.

## S1 Current-State Findings

Reusable capabilities:

- healthy first-party local WeChat Worker with local-only session ownership;
- an existing source subscription and durable cursor for the selected account;
- idempotent article ingestion, bounded chunks, citations, and search;
- knowledge analysis, quality, immutable Releases, Agent Packages, trusted
  evaluation, and online runtime traces;
- explicit human-controlled publication and rollback patterns.

Production observations made without retaining article bodies or credentials:

- the selected account already has 219 normalized source-ingest packages;
- 219 packages carry `source_type=wechat_mp_article` and are ready;
- two additional manual-import drafts are outside the canonical source-ingest
  identity path;
- the active Worker reports healthy WeChat capability and an empty outbox;
- the most recent account runs stopped on upstream throttling;
- the existing generic compiler accepts only a small supporting-Release set and
  is not a scalable account archive contract.

Hard constraints:

- “all” means all history visible to the current authorized session, not an
  unverifiable claim about deleted, private, or upstream-hidden posts;
- no rate-limit evasion, verification bypass, credential export, or public
  redistribution;
- article acquisition can be automated, but collection and Agent publication
  remain explicit operator actions;
- health content is always `evidence_only`;
- preserve unrelated dirty-worktree changes and never commit runtime content.

## G1 Admission

**Decision:** PASS

The request directly extends the product's source-to-knowledge and Agent supply
chain. The smallest coherent slice is one immutable account collection Release
and one evaluated Agent that searches its pinned member packages.

## S2 Product Definition

Approved scope:

- backfill all currently visible account history;
- deduplicate and resume through the existing source subscription;
- synchronize future articles daily;
- add first-class account collection definitions, candidates, quality reports,
  Releases, Agent scope, runtime retrieval, and Web controls;
- publish and test the first real account collection Agent in production.

Approved design:

- [`../plans/2026-08-12-wechat-account-collection-agent-design.md`](../plans/2026-08-12-wechat-account-collection-agent-design.md)

## G2 Feasibility and Risk Review

**Decision:** PASS WITH HARD CONSTRAINTS

The existing Worker and source store make bounded, restart-safe acquisition
feasible. A new immutable collection Release avoids merging all content into a
synthetic book and avoids a live mutable Agent scope. Risks are controlled by
deterministic membership hashes, source-account filtering, citation checks,
throttle cooldown, evidence-only policy, explicit publication, and retention of
the last known-good Agent.

## S3 Plan

Implementation plan:

- [`../plans/2026-08-12-wechat-account-collection-agent.md`](../plans/2026-08-12-wechat-account-collection-agent.md)

## Gate Ledger

| Gate | Status | Evidence | Next action |
| --- | --- | --- | --- |
| G1 Admission | PASS | User approved complete visible history, continued synchronization, and account collection Agent scope | Preserve approved slice |
| G2 Feasibility/risk | PASS WITH CONSTRAINTS | Existing Worker, cursor, article packages, Releases, and Agent runtime are reusable; immutable collection design approved | Implement with TDD |
| G3 Tests | PENDING | No implementation yet | Run focused tests, full Go tests, frontend build, smokes, privacy, and diff checks |
| G4 Review | PENDING | Collection integrity, auth, privacy, medical boundary, and runtime scope require independent review | Review before deployment |
| G5 Deploy health | PENDING | No release artifact yet | Deploy from clean reviewed branch with rollback point |
| G6 Production validation | PENDING | Existing runtime inventory is not a published collection Agent | Backfill, publish, ask real questions, and verify citations |

## Runtime Data Boundary

The selected account name and observed counts are recorded only as operational
evidence in this Dossier. Article titles, bodies, URLs, account keys, cookies,
tokens, media, and downloaded packages must remain outside Git.
