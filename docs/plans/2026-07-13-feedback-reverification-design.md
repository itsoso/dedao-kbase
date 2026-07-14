# Feedback-Driven Reverification Design

**Status:** Approved as the next source-to-knowledge closed-loop slice.

## Goal

Turn immutable release feedback into a deterministic, inspectable signal that
tells operators when a published release needs re-verification. The signal must
never mutate source content, rewrite an existing release, or publish a new
release automatically.

## Options Considered

1. Let each consumer decide whether a release is stale. This is simple, but
   fragments policy and gives KBase no global view.
2. Add a separate queue service. This scales later, but adds infrastructure
   before feedback volume requires it.
3. **Recommended:** derive an assessment from KBase's append-only feedback log.
   It is deterministic, idempotent, auditable, and fits the existing store.

## Contract

Each release exposes a feedback assessment with aggregate counts, a disposition
(`healthy` or `reverify_required`), machine-readable trigger outcomes, and the
latest feedback timestamp. Any `stale`, `conflict`, or `rejected` event requires
re-verification. `used` is positive evidence. `zero_hit` remains a coverage
signal and does not invalidate a release by itself.

`POST /api/knowledge/releases/{release_id}/feedback` returns the new assessment.
`GET` on the same resource returns the current assessment without exposing raw
event IDs or consumer identifiers. The immutable release stays available so
consumers can keep enforcing their own policy while an operator investigates.

## Safety And Failure Handling

- Only bounded enums and opaque identifiers are persisted.
- Assessment is rebuilt from the append-only log; no mutable queue state exists.
- Missing releases return 404. Invalid feedback remains 400/409.
- Read or write failures are visible; there is no silent fallback.
- Re-verification creates follow-up work only. Analysis, quality verification,
  and publication continue through their existing explicit gates.

## Verification

Unit tests cover empty, positive, stale, conflict, rejection, zero-hit, and
idempotent feedback. HTTP tests cover authenticated GET/POST responses and
privacy-safe serialization. Repository Go tests, frontend smoke checks,
privacy smoke, and diff checks remain release gates.
