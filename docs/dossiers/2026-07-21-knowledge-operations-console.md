# Knowledge Operations Console Dossier

**Status:** IN PROGRESS — G1/G2 PASS, G3/G4 pending

## S0 · User request

Approved. Continue with the Knowledge Operations Console direction and
first-release scope: Release Status Center, Health Evidence Review Workspace,
and Failure Explanation / Safe Replay.

Required boundaries:

- no automatic Health serving promotion;
- no source body exposure;
- no unsafe replay;
- explicit confirmation for dangerous actions;
- stop on failed Gate, genuine blocker, destructive action, or required secret
  or human decision.

## S1 · Discovery

- Repository instructions and `docs/system-map/INDEX.md` were read before
  implementation.
- Existing KBase surfaces already provide pipeline dashboard, immutable
  releases, quality reports, Health evidence readiness/evidence packages,
  feedback, and reverification retry.
- Existing frontend-web already has a knowledge workbench, pipeline panel,
  review panel, and agent package pages.
- Current feature branch was clean before this feature started.

## G1 · Admission

- first_class_objects: Knowledge Operations Console, operations item, Health
  review summary, failure explanation, safe replay request/result.
- core_loop_step: observe release state -> identify Health review blockers ->
  explain failure -> safely replay analysis or quality only.
- smallest_end_to_end_slice: one authenticated operator API and page showing
  one package's pipeline, release, Health readiness, failure explanation, and
  safe replay affordance.
- risk: medium-high because Health evidence and replay controls are involved.
- decision: PASS. Scope is visibility plus bounded replay and preserves
  consumer ownership.

## S2 · Product/design

Design document:
`docs/plans/2026-07-21-knowledge-operations-console-design.md`.

Decision: a KBase-owned operations aggregation API and frontend page, not a
Health serving or review mutation API.

## S3 · Plan

Implementation plan:
`docs/plans/2026-07-21-knowledge-operations-console.md`.

## G2 · Feasibility and safety

- Reuses existing KBase stores and functions.
- Operations API returns metadata and aggregate counts only; it does not include
  source bodies, prompts, tokens, or cookies.
- Replay allows only analysis and deterministic quality evaluation. Publish,
  Health serving promotion, feedback writes, and unknown actions fail closed.
- Health review ownership remains outside KBase; KBase reports readiness and
  evidence metadata only.
- decision: PASS.

## Checkpoints

Implementation is starting from Task 2 with TDD.
