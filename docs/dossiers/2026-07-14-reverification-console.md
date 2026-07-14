# Knowledge Reverification Console Dossier

**Status:** Implementing

## Requirement

Continue the feedback-driven knowledge loop with an operator-facing review
console for asynchronous candidates.

## Scope

- Compact task status in the current book knowledge page.
- Candidate/release and quality evidence review.
- Book-scoped release lookup that is not truncated by global pagination.
- Safe manual retry for failed current tasks.
- Explicit, confirmed publication through the existing gate.

Out of scope: automatic publication, feedback deletion, raw model errors,
release mutation, and a separate administration application.

## Artifacts

- Design: `docs/plans/2026-07-14-reverification-console-design.md`
- Plan: `docs/plans/2026-07-14-reverification-console.md`

## Gates

- **G1 Admission:** PASS. Durable candidates need an operator action surface to
  complete the approved human publication loop.
- **G2 Feasibility and risk:** PASS WITH BOUNDARY. Existing release, quality,
  feedback, task, and publish APIs provide the evidence. Only failed-task retry
  is new; publication remains explicit and backend-gated.
- **G3 Test:** PENDING.
- **G4 Review:** PENDING.
- **G5 Deployment health:** PENDING.
- **G6 Online verification:** PENDING.

## Current Stage

S4 decomposition complete; entering test-driven implementation.
