# Knowledge Supply Platform Design

**Status:** Approved

## Decision

Keep KBase as one deployable control plane with local edge agents. Consolidate
its existing source, knowledge-package, quality, release, feedback, and
reverification capabilities behind a common object model and observable
pipeline. Do not split collection, compilation, and registry into separate
services until measured load or isolation needs justify it.

## System Boundary

```text
local source agents
  -> source/content ledger
  -> knowledge compiler
  -> deterministic quality gate
  -> immutable release registry
  -> downstream domain import/review
  -> reviewed serving index
  -> privacy-safe receipts and feedback
  -> KBase impact, gap, and reverification queues
```

KBase is an authoring and release plane. Downstream systems remain the serving
plane. A health consumer imports only published releases, performs claim-level
domain review and external evidence checks, then serves only reviewed records.

## Canonical Objects

- `SourceIdentity`: adapter, remote account/object key, canonical URI, license
  scope, and ownership boundary.
- `ContentVersion`: source ID, normalized content hash, acquisition metadata,
  predecessor, and immutable package reference.
- `PipelineProjection`: current stage and last run for each content version;
  derived from existing durable run and artifact records.
- `KnowledgeCandidate`: structured claims, risks, actions, citations, model,
  prompt version, content hash, and quality report.
- `KnowledgeRelease`: immutable published candidate with supersession and usage
  policy.
- `DeliveryReceipt`: consumer, release, idempotency key, disposition, imported
  fingerprint, and timestamp; never user data.
- `FeedbackEvent`: bounded claim-level outcome already supported by the release
  feedback contract.
- `KnowledgeGap`: aggregated zero-hit or unsupported demand represented by a
  fingerprint and count, without raw queries.

## Storage Strategy

Immutable source packages, quality reports, releases, and feedback remain the
facts of record. Add a SQLite catalog for joins, filters, delivery receipts,
pipeline projections, and aggregates. The catalog is rebuildable from durable
artifacts and source-run records; it must never become the only copy of release
content.

This avoids a risky storage migration while removing repeated filesystem scans
from control-plane reads. Move blobs to object storage or split services only
after explicit volume, concurrency, or availability thresholds are breached.

## Contracts

Keep existing release URLs compatible and make schema evolution explicit in
payloads. Add:

```text
GET  /api/knowledge/feed?after={cursor}&limit={n}&source={kind}&policy={policy}
POST /api/knowledge/releases/{release_id}/receipts
GET  /api/knowledge/lineage/{object_id}
GET  /api/knowledge/pipeline?stage={stage}&status={status}
GET  /api/knowledge/impact
GET  /api/knowledge/gaps
```

Contract JSON Schemas and fixtures live in the repository. Consumer contract
tests validate release identity, citation lineage, evidence-only policy,
idempotent cursor handling, and receipt replay before either side deploys.

Delivery is pull-based first: consumers advance a durable cursor only after
their import transaction succeeds, then post an idempotent receipt. Webhooks can
be added later as a wake-up hint but never replace the feed as the recovery path.

## Pipeline And Automation

The common projection is:

`collected -> normalized -> analyzed -> verified -> candidate -> published`

Each stage records input fingerprint, implementation version, attempt,
timestamps, public error code, and output reference. Existing adapter runs and
reverification tasks remain durable owners of execution; the projection does
not introduce a second scheduler.

Automation may collect, normalize, analyze, evaluate quality, retry bounded
failures, create candidates, deliver published releases, aggregate gaps, and
open refresh/reverification work. Publication remains explicit. A consumer's
domain approval and reviewed-serving publication remain outside KBase.

## Quality And Governance

Extend deterministic quality checks incrementally:

- content and analysis hash binding;
- citation existence and claim-to-citation resolution;
- claim atomicity, scope, confidence, and risk classification;
- bounded transformed excerpts and license scope;
- freshness policy by source/domain;
- contradiction and supersession signals;
- high-risk `evidence_only` enforcement.

KBase reports these facts but does not encode consumer-specific medical or legal
policy. Consumers can hold or reject otherwise valid releases and return the
decision through receipts or feedback.

## Control Plane

- **Sources:** identity, subscriptions, freshness, versions, duplicate groups.
- **Pipeline:** stage, age, attempts, terminal error, bulk retry/cancel.
- **Review:** candidate/release diff, rules, claims, explicit publication.
- **Releases:** immutable history, supersession, lineage, delivery receipts.
- **Impact:** imports, usage, zero hits, conflicts, stale age, gap backlog.

Views share URL-addressable filters and never duplicate source-specific action
logic. Raw local-agent diagnostics remain a secondary operations surface.

## Observability And Failure Semantics

Every cross-system operation carries release ID, content hash, and idempotency
key. Metrics aggregate counts and age without content bodies. Errors are stable
codes with operator guidance; no silent fallback changes serving truth.

The key service-level indicators are pipeline terminal-failure age,
publish-to-import latency, receipt failure age, stale/conflict resolution time,
zero-hit backlog age, and published-claim citation validity.

## Security And Privacy

- Local credentials remain in Keychain or server secret configuration.
- Admin and source-agent tokens remain separate; consumer identities become
  scoped service credentials before additional consumers are enabled.
- Feedback, gaps, receipts, and metrics exclude raw prompts, answers, personal
  records, cookies, filesystem paths, and downloaded source bodies.
- Release exports remain bounded transformed knowledge, not a content mirror.

## Delivery Order

1. System map and contract schemas.
2. Rebuildable catalog and pipeline projection.
3. Incremental feed, delivery receipts, and lineage.
4. Health pilot contract and impact metrics.
5. Gap automation and freshness policy.
6. Additional consumer adapters or infrastructure split only from measured need.

