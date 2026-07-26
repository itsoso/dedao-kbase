# Knowledge Foundation v2 Delivery Dossier

## Status

Implementation in progress on branch `codex/knowledge-foundation-v2`.

## User Requirement

> 继续从第一性原理出发，思考如何做一个知识库
>
> 写成详细的开发文档，然后执行

## Outcome

Build the smallest compatible foundation that makes KBase knowledge
verifiable: deterministic evidence chains, conservative publication identity,
quality and publication gates, and a corpus-wide readiness API.

## Current-System Findings

- Normalized packages already contain books, chapters, chunks, claims, and
  citations.
- Structured analysis already emits claims with source IDs.
- Quality evaluation currently checks source IDs against a flat allowed-ID set.
- Knowledge Releases are immutable, content-addressed, and hash-bound to
  analysis and quality.
- Agent Packages, Health feeds, Proofroom projection, receipts, feedback, and
  reverification already consume Releases.
- The missing shared layer is graph integrity and corpus-wide evidence
  observability.

## Approved Scope

- Add `knowledge_evidence.v1` as an internal deterministic report.
- Add `knowledge_readiness.v1` as an authenticated read-only API contract.
- Preserve `knowledge_release.v1`.
- Accept resolvable direct chunk references as an explicit migration warning.
- Block unresolved, conflicting, or cross-book evidence during new publication.
- Do not rewrite existing Release artifacts.

## Gate Decisions

### G1 - Admission

PASS. Evidence integrity directly supports the product goal: every book can
become a trustworthy Agent knowledge source. The smallest end-to-end slice is
validator -> quality -> publish gate -> readiness API.

### G2 - Feasibility And Risk

PASS with controls.

- Reuse existing package, quality, release, and pipeline stores.
- Keep validation pure and deterministic.
- Keep the change additive and release-schema compatible.
- Never expose raw content, prompts, credentials, or local absolute paths.
- Do not claim independent corroboration from item-level IDs.
- Do not auto-publish or transfer consumer domain authority.

### G3 - Tests

Pending implementation.

### G4 - Review

Pending implementation and independent review because this changes publication
and external API behavior.

### G5 - Deployment Health

Pending explicit deployment decision after clean-main integration.

### G6 - Online Verification

Pending production verification with synthetic or authorized metadata-only
fixtures.

## Artifacts

- PRD: `docs/prd/2026-07-25-knowledge-foundation-v2.md`
- Design: `docs/plans/2026-07-25-knowledge-foundation-v2-design.md`
- Plan: `docs/plans/2026-07-25-knowledge-foundation-v2.md`
- System map: `docs/system-map/INDEX.md`

## Implementation Checkpoints

- Task 1 - Documentation: in progress.
- Task 2 - Evidence graph contract: pending.
- Task 3 - Quality and publication gates: pending.
- Task 4 - Readiness projection: pending.
- Task 5 - HTTP and JSON Schema contract: pending.
- Task 6 - full verification and review: pending.

## Rollback

Remove the readiness route and evidence re-check from a clean-main rollback
commit. No data migration or mutation is required; existing Releases remain
valid and readable.
