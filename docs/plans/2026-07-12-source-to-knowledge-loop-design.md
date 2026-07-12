# Source-to-Knowledge Closed Loop Design

**Status:** Approved

## Goal

Turn KBase into a domain-neutral knowledge production and supply system. It
collects content from Dedao, WeChat, files, and future adapters; converts it
into versioned evidence; publishes only machine-verifiable releases; and
accepts downstream feedback from consumer systems.

The first consumer is a health assistant. High-risk health content may be
published as evidence, but KBase must never promote it directly into personal
medical advice.

## Ownership Boundary

KBase owns:

- source identity, collection runs, immutable content versions, and provenance;
- normalization into chapters, chunks, citations, and claims;
- model-assisted analysis with prompt/model/content version recording;
- deterministic quality gates and immutable knowledge releases;
- release retrieval and consumer feedback contracts.

Consumer systems own:

- user context and permissions;
- domain policy, safety rules, and decision logic;
- final advice or actions;
- feedback describing whether evidence was used, rejected, stale, or conflicting.

## Pipeline

```text
collected -> normalized -> citation_ready -> analyzed -> verified -> published
     |            |              |              |           |
     +----------> failed / quarantined / stale <------------+

published -> consumed -> feedback_received -> re-evaluated
```

Collection never waits for an external model. A successful source ingest
creates a pending analysis manifest tied to the content hash. Analysis,
verification, and release are retryable stages outside the ingestion
transaction.

## Knowledge Objects

### Structured analysis

The current Markdown answer remains a human-readable rendering. The durable
payload adds:

- `summary`: bounded source-grounded abstract;
- `claims`: atomic statements with citation IDs, confidence, scope, and risk;
- `risks`: limitations, conflicts, and missing external verification;
- `actions`: reading or investigation actions, never personal medical advice;
- `model`, `prompt_version`, `content_hash`, and timestamps.

### Quality report

The quality gate is deterministic and does not trust the generating model. It
checks:

- content hash matches the current package;
- every claim has valid citation IDs belonging to the package;
- confidence and risk values are valid;
- no empty or implausibly short evidence package is published;
- high-risk claims are explicitly marked as evidence-only;
- conflicting or stale content is quarantined rather than silently served.

The report contains rule-level outcomes and an overall `pass`, `quarantine`, or
`reject` decision.

### Knowledge release

A release is immutable and content-addressed. It contains the source metadata,
structured analysis, quality report, citations, and package/release versions.
Publishing a newer content hash creates a new release and supersedes the old
one without deleting historical evidence.

### Consumer feedback

Consumers report `used`, `rejected`, `stale`, `conflict`, or `zero_hit`, along
with referenced claim IDs and a non-sensitive reason. Feedback never contains
personal health records. It is aggregated to trigger re-analysis,
quarantine, or source refresh.

## API Contract

Initial authenticated endpoints:

```text
GET  /api/books/{book_id}/analysis
POST /api/books/{book_id}/analysis
GET  /api/books/{book_id}/quality
POST /api/books/{book_id}/publish
GET  /api/knowledge/releases?after={cursor}&limit={n}
GET  /api/knowledge/releases/{release_id}
POST /api/knowledge/releases/{release_id}/feedback
```

Consumer APIs return only published releases. Draft manifests and rejected
quality reports remain administrative surfaces.

## Automation Policy

Analysis may run automatically after ingestion. Publication is automatic only
when every hard quality rule passes. Health-related high-risk releases carry
`usage_policy=evidence_only`; consumer systems must enforce their own safety
and user-context gates before producing advice.

Failed jobs use bounded retries with visible terminal errors. Repeated model
failures do not delete the previous successful release. Content updates make
the prior release stale until a new version passes verification.

## Observability

The control plane must expose counts and age for pending, running, failed,
quarantined, and published items, plus consumer zero-hit, conflict, rejection,
and stale feedback. The key product metric is not article count; it is the
share of published claims that are retrieved, cited, and accepted by consumers.

## Initial Delivery Slice

1. Extend `analysis_manifest.json` with structured claims, risks, and actions.
2. Add deterministic quality reports.
3. Add immutable release storage and release REST reads.
4. Add consumer feedback persistence.
5. Connect the health assistant through the release contract in a later repo
   change after the KBase producer contract is stable.

