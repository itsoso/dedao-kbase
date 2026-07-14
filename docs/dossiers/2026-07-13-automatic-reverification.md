# Automatic Knowledge Reverification Dossier

**Status:** Implementing

## Requirement

Continue the feedback-driven knowledge loop with automatic, durable
reverification work after invalidating consumer feedback.

## User And Value

Knowledge consumers need authoritative releases to improve from observed stale,
conflicting, or rejected evidence without relying on an operator to monitor every
feedback event. Existing releases must remain stable until a replacement passes
quality gates and is explicitly published.

## Scope

- Durable and idempotent reverification tasks.
- Asynchronous candidate analysis and quality evaluation.
- Authenticated aggregate task visibility.
- Restart recovery, coalescing, cooldown, and bounded failures.

Out of scope: automatic publication, release deletion, consumer identity or free
text storage, and synchronous model work in the feedback request.

## Artifacts

- Design: `docs/plans/2026-07-13-automatic-reverification-design.md`
- Plan: `docs/plans/2026-07-13-automatic-reverification.md`

## Gates

- **G1 Admission:** PASS. This closes the approved `feedback_received ->
  re-evaluated` loop with a bounded end-to-end slice.
- **G2 Feasibility and risk:** PASS. Existing analysis and quality components are
  reusable. Hard boundaries are durable enqueue, one active task per release,
  no synchronous model invocation, no automatic publication, and no raw consumer
  data in task responses.
- **G3 Test:** PENDING.
- **G4 Review:** PENDING. Authentication, model-cost amplification, task races,
  privacy, and publication boundaries require independent review.
- **G5 Deployment health:** PENDING.
- **G6 Online verification:** PENDING.

## Current Stage

S4 requirement decomposition complete; entering S5 test-driven implementation.
