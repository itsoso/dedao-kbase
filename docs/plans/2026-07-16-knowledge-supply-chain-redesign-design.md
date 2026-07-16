# Knowledge Supply Chain Redesign Design

**Status:** Approved for implementation

## Decision

KBase should become the knowledge supply chain control plane, not a crawler
bundle and not the final domain application. It should own source acquisition,
normalization, versioning, quality gates, release packaging, and consumer
delivery contracts. Downstream health and research consumers should import
reviewed, traceable knowledge releases instead of reading raw Dedao or WeChat
content directly.

Keep the current one-service deployment and local-agent model. Add explicit
interfaces and contracts before introducing more infrastructure. Split into
separate services only when measured workload, credential isolation, or
availability constraints require it.

## Best-Practice Inputs

The design follows common patterns from established open-source knowledge and
data platforms:

- LangChain-style document loaders: every source exposes a common load contract
  and source metadata instead of leaking site-specific collection logic upward.
- Unstructured-style document processing: partition, clean, chunk, enrich, and
  embed as separate, inspectable stages.
- Haystack-style RAG pipelines: retrieval, ranking, routing, generation, and
  evaluation are modular, measurable steps.
- DataHub, Marquez, and OpenLineage-style governance: lineage, freshness,
  ownership, quality, and downstream usage are first-class metadata.

## Target Architecture

```text
Source connectors
  -> acquisition ledger
  -> normalization and dedupe
  -> knowledge package builder
  -> search and retrieval index
  -> quality and evaluation gates
  -> immutable release registry
  -> consumer-specific feed packages
  -> receipts, feedback, gaps, and impact
```

KBase remains source-agnostic after the connector boundary. Dedao ebooks,
courses, WeChat public-account articles, manual uploads, and future sources all
produce the same canonical source document shape.

## Canonical Contracts

- `SourceConnector`: source-specific adapter with capabilities, checkpoint,
  fetch, and normalize operations.
- `SourceDocument`: normalized title, author, source URI, timestamps, license
  scope, content hash, content format, and metadata.
- `KnowledgePackage`: chapters, chunks, claims, citations, source lineage, and
  quality metadata.
- `SearchIndex`: keyword and semantic retrieval surface over packages and
  source documents.
- `ConsumerRelease`: scoped package generated for one consumer policy, such as
  health evidence-only or proofroom citation-first.
- `EvaluationRun`: retrieval and answer-quality metrics tied to package version,
  prompt version, model, and consumer policy.

## Current Gaps

The current system has source ingest, catalog, releases, feed, receipts,
lineage, impact, gaps, reverification, and a UI cockpit. It still lacks:

- one formal connector contract shared by Dedao, WeChat, and future sources;
- a hybrid search foundation with source filters, freshness filters, and
  consumer policy filters;
- health-specific release envelopes and import fixtures;
- an evaluation harness for retrieval quality, faithfulness, and citation
  coverage;
- automatic change-impact planning from source update to consumer rebuild;
- a productized permission model for source rights and downstream usage scope.

## Health Consumer Boundary

For the health consumer, KBase should provide evidence candidates, not medical
truth. The health system must keep domain review, safety policy, diagnosis
guardrails, and user-context decisions. KBase provides:

- stable release IDs and content hashes;
- claim, citation, source, and freshness metadata;
- evidence-only usage policy;
- changed-since feed;
- import receipts and rejection feedback;
- gap and impact summaries.

## Governance Rules

1. Raw source credentials stay in local agents or server secrets.
2. Releases contain bounded transformed knowledge, not mirrored source bodies.
3. Every claim must trace to a chunk and source.
4. Every consumer import must be idempotent and receipt-backed.
5. Quality failures block publication; consumer rejections open feedback loops.
6. Evaluation scores are advisory until deterministic gates pass.

## Delivery Strategy

Implement in layers that preserve the current production system:

1. Define connector contracts and adapt existing sources without changing
   public routes.
2. Add search-index primitives and rebuild commands.
3. Add health-specific release feed and fixtures.
4. Add evaluation harness and UI visibility.
5. Add change-impact automation and rebuild recommendations.
6. Extend to additional consumers and richer source types.
