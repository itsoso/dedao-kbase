# Controlled Book Agent Wizard Design

## Goal

Complete the human-controlled path from an authorized Dedao ebook to a
published knowledge release and an evaluated Book Agent without exposing the
dedicated Agent Package publisher credential to the browser.

## Current Failure

The fallback Dedao ebook importer can persist a complete package with chapters,
chunks, claims, and citations while leaving `book.content_hash` empty. Analysis
then records the same empty value, and the deterministic `content_version`
quality rule rejects the package. The Web console also has no initial knowledge
release action and no guided Agent Package authoring surface.

## Architecture

The implementation keeps the existing source, knowledge release, Agent Package,
evaluation, and publication boundaries.

1. The knowledge compiler assigns a deterministic content hash before saving any
   newly imported package. A narrow repair operation recomputes the hash for a
   legacy package only when its hash is missing; it never rewrites an existing
   non-empty identity.
2. The knowledge workspace exposes the initial release path after analysis and
   quality evaluation pass. Publication remains an explicit, confirmed action
   and produces the existing immutable knowledge release.
3. A Book Agent wizard derives a draft Agent Package from one published release.
   The user reviews identity, read-only capabilities, retrieval/model limits,
   safety policy, and a bounded golden evaluation suite.
4. The browser submits the reviewed draft to a narrow server-side controlled
   workflow. The server owns the dedicated publisher credential, evaluates the
   immutable package content, and publishes only after all declared thresholds
   pass. The publisher credential is never returned to or stored by the browser.

## Content Identity And Repair

The content hash covers normalized book metadata and durable knowledge objects
that define retrieval behavior: chapters, chunks, claims, citations, entities,
and relationships. Volatile timestamps, local paths, credentials, and generated
analysis are excluded.

The repair path loads the existing package, verifies that the hash is empty,
computes the canonical hash, persists the package atomically, and invalidates
stale analysis and quality artifacts. The user must regenerate analysis against
the repaired identity before publication. This avoids silently relabeling an
old analysis as current.

## Initial Knowledge Release

The workspace shows a deterministic progression:

`content ready -> analysis required -> quality pass/reject -> publish release`

When no release exists, a passing current quality report enables an explicit
"Publish knowledge release" action. Rejection lists the failed rules and leaves
publication disabled. A confirmation dialog precedes the mutation. Replays are
idempotent through the existing release contract.

## Controlled Agent Wizard

The wizard has three steps:

1. Select one published knowledge release and inspect its content identity and
   citation count.
2. Configure package name, version, read-only capabilities, retrieval limit,
   model capability, cost/timeout bounds, and safety/abstention policy. The first
   version supports `reader`, `search`, `grounded_chat`, and `evidence` only.
3. Review a generated bounded evaluation suite, run evaluation, inspect scores,
   and explicitly confirm publication.

The workflow generates contract-valid manifests and uses the existing Agent
Package evaluator and store. Evaluation failure is a terminal hold for that
attempt; it cannot be bypassed by the UI. Publication remains idempotent for the
same package content and idempotency key.

## Authorization

Normal read operations continue to use the shared consumer token. Controlled
Agent evaluation and publication are handled only inside the server and require
the configured dedicated publisher authority. The browser receives status,
validation failures, evaluation metrics, and published identifiers, but never
receives credentials.

## Error Handling

- Missing or malformed source content fails before a hash is persisted.
- Existing non-empty content hashes are never replaced by the legacy repair.
- Stale analysis or quality reports disable release publication.
- Missing publisher configuration disables the final Agent action with a clear
  configuration error.
- Evaluation failures preserve the draft inputs and report the failed metrics.
- No stage silently falls back to weaker retrieval, missing citations, or an
  unevaluated publication.

## Verification

- Unit tests reproduce the empty-hash Dedao fallback import and prove stable
  hashing and safe legacy repair.
- HTTP tests prove initial release visibility, stale-analysis rejection,
  confirmation requirements, and publisher-boundary enforcement.
- Frontend smoke tests prove the lifecycle actions and three wizard states.
- Contract tests validate generated Agent Packages and golden suites.
- Full Go and frontend builds, privacy checks, deployment health checks, and
  online end-to-end verification gate release.

## Non-Goals

- No browser storage of publisher credentials.
- No autonomous publication or quality-gate bypass.
- No write-capable MCP tools in the first Book Agent version.
- No vector or hybrid retrieval until an authorized embedding identity is
  explicitly configured and evaluated.
