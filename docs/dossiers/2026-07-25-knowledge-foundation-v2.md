# Knowledge Foundation v2 Delivery Dossier

## Status

Implementation and local verification complete on branch
`codex/knowledge-foundation-v2`. Awaiting clean-main integration, deployment,
and online verification.

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

PASS on 2026-07-25.

- `npm --prefix frontend run build`
- `go test ./... -count=1`
- `go test -race ./backend/app ./cmd/kbase-server -count=1`
- `go vet ./...`
- `go mod verify`
- `bash scripts/knowledge-contract-smoke.sh`
- `node frontend/scripts/markdown-render-smoke.mjs`
- `node frontend/scripts/book-knowledge-ui-smoke.mjs`
- generated system-map drift check
- `bash scripts/privacy-smoke.sh`
- `git diff --check`

The frontend build retains the existing dependency `eval` and large-chunk
warnings. The race build retains the existing macOS `LC_DYSYMTAB` linker
warning. All commands exited successfully; no race report or test failure was
emitted.

### G4 - Review

PASS after remediation on 2026-07-25.

The first independent review returned BLOCK:

- a citation could terminate at a chunk without a chapter;
- readiness exposed raw `source_account`;
- readiness summary counted only the `limit`-truncated item set.

The implementation was returned to S5. Failing regression tests were added
before fixing each issue. Chunks now require a resolvable chapter, readiness
exposes only the bounded canonical publication identity, and summary metrics
cover every matching package while `limit` bounds only returned items.

The second review found no remaining P1/P2 implementation issue. Its final
conditional blocker was the uncommitted generated system map. Commit `c09f4a7`
added the generated route/type inventory, and the drift check passed from a
clean worktree.

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

- Task 1 - Documentation: completed in `a92d1b5`.
- Task 2 - Evidence graph contract: completed in `62bfc63` and hardened in
  `7be9f0a`.
- Task 3 - Quality and publication gates: completed in `02b237a`.
- Task 4 - Readiness projection: completed in `2a250b4`.
- Task 5 - HTTP and JSON Schema contract: completed in `5390cf5`.
- Task 6 - review remediation and generated system map: completed in `2acd71b`
  and `c09f4a7`.

## Delivered Behavior

- Deterministic package identity and evidence graph validation.
- Complete citation-to-chunk-to-chapter ownership checks.
- Conservative publication identity with independence eligibility.
- Quality quarantine/reject rules for unresolved and structurally unsafe
  evidence.
- Publication-time evidence revalidation independent of stored quality state.
- Authenticated `GET /api/knowledge/readiness` with corpus-wide aggregate
  coverage and bounded item projection.
- `knowledge-readiness-v1.schema.json` and supply-contract documentation.

## Rollback

Remove the readiness route and evidence re-check from a clean-main rollback
commit. No data migration or mutation is required; existing Releases remain
valid and readable.
