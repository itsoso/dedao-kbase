# Research Agent Platform Dossier

**Date:** 2026-08-13

**Status:** Layer A in progress; G1 passed; G2–G6 pending

**Approved design:**
[Research Agent Platform Design](../plans/2026-08-13-research-agent-platform-design.md)

**Implementation plan:**
[Research Agent Platform Implementation Plan](../plans/2026-08-13-research-agent-platform.md)

## Objective and boundaries

Build a platform-level, dual-mode Research Agent runtime. Quick mode answers a
bounded question against a selected immutable knowledge package. Deep mode can
coordinate versioned knowledge, prior verified Research Runs, and a local
read-only Chatlog Worker to resolve identities, reconstruct timelines, compare
cases, detect conflicts, and publish a citation-verified report.

Complete Chatlog data remains on the local machine. The server may persist a
structured run, opaque locators and hashes, and only the minimal evidence
excerpts selected for the report. The Worker uses the existing shared Worker
token. This feature does not add request signing, message sending, message
editing, message deletion, bulk private-data export, or automatic publication.

## Delivery layers

1. Layer A: run contracts, durable store, and evidence privacy boundary.
2. Layer B: local Chatlog Worker and restricted macOS delivery.
3. Layer C: retrieval tools and role-separated orchestration.
4. Layer D: versioned Agent policy and Chinese operator workspace.
5. Layer E: gold evaluation, controlled release, and production proof.

## Gate decisions

| Gate | Status | Evidence |
|---|---|---|
| G1 Admission | PASS | Approved design, explicit modes, source boundaries, transition rules, budgets, and typed deep-research escalation are captured by Task 1 tests. |
| G2 Feasibility and risk | PENDING | Loopback API feasibility is known; production Worker and privacy boundaries are not yet implemented. |
| G3 Tests | PENDING | Clean worktree baseline passed before implementation. |
| G4 Review | PENDING | No implementation review has occurred. |
| G5 Deployment health | PENDING | Nothing from this feature has been deployed. |
| G6 Online verification | PENDING | No production Research Run has been executed. |

## Current checkpoint

Task 1 is complete in an isolated feature worktree. Its focused Research Run
and routing tests pass. Task 2 begins the durable store. No later gate may be
marked PASS before its stated evidence exists, and a failed gate returns to the
responsible upstream task.
