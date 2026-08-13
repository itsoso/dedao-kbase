# WeChat Account Collection Agent Dossier

## Status

- **Current stage:** S5 — deployment pending
- **Delivery status:** implementation and review passed; production rollout pending
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

## S4 Implementation and Verification

The implementation now provides exact-account collection definitions,
deterministic candidates and quality reports, immutable collection Releases,
Agent Package v3 collection scope, pinned cross-article retrieval and citation
resolution, trusted evaluation, and a Chinese operator workspace. Collection
and Agent publication remain separate explicit browser-session actions.

The source scheduler now persists typed upstream-throttle cooldowns and resumes
only after the bounded retry time. Authentication, verification, forbidden,
and permanent failures remain blocked. Cursor state is preserved across a
throttled discovery failure.

Fresh release-gate evidence on the final feature branch:

```text
go test ./... -count=1 -timeout=30m
go vet ./...
go test -race ./backend/app ./cmd/kbase-server ./cmd/source-agent -count=1 -timeout=30m
cd frontend && npm run build
all frontend/scripts/*-smoke.mjs
node --check frontend-web/app.js
all frontend-web/scripts/*smoke*.mjs
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
bash scripts/evolution-workers-deployment-smoke.sh
bash scripts/source-agent-control-plane-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
PASS
```

The Vue build retained its existing large-chunk warning but exited successfully.
The first full Web smoke run found that a collection-list failure could abort
the existing source-control refresh before stale cross-subscription run state
was cleared. A RED/GREEN regression now proves collection-list failure is
isolated and visible while Agent, subscription, and run history remain usable.

The review checked authentication and CSRF coverage, member-count bounds,
source-account isolation, content and candidate hashes, citation allowlists,
runtime provenance, medical `evidence_only`, explicit confirmation, and the
absence of credentials or downloaded article bodies in Git. No unresolved
Critical, High, or Medium release blocker remains.

## Gate Ledger

| Gate | Status | Evidence | Next action |
| --- | --- | --- | --- |
| G1 Admission | PASS | User approved complete visible history, continued synchronization, and account collection Agent scope | Preserve approved slice |
| G2 Feasibility/risk | PASS WITH CONSTRAINTS | Existing Worker, cursor, article packages, Releases, and Agent runtime are reusable; immutable collection design approved | Implement with TDD |
| G3 Tests | PASS | Full Go tests, focused race tests, vet, Vue build, all Web smokes, deployment smokes, system-map, privacy, and whitespace checks passed | Preserve exact revision evidence |
| G4 Review | PASS | Exact account scope, 500-member bound, hash/citation integrity, browser Cookie+CSRF publication, medical `evidence_only`, and no-content/no-secret Git boundary reviewed | Deploy only from clean reviewed revision |
| G5 Deploy health | PENDING | Production remains on the previous healthy revision | Deploy from clean reviewed branch with rollback point |
| G6 Production validation | PENDING | Existing runtime inventory is not a published collection Agent | Backfill, publish, ask real questions, and verify citations |

## Runtime Data Boundary

The selected account name and observed counts are recorded only as operational
evidence in this Dossier. Article titles, bodies, URLs, account keys, cookies,
tokens, media, and downloaded packages must remain outside Git.
