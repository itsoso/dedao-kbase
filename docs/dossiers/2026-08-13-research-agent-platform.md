# Research Agent Platform Dossier

**Date:** 2026-08-13

**Status:** Layer E in progress; G1–G2 passed; G3–G6 pending

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
| G2 Feasibility and risk | PASS | The macOS Chatlog Worker, shared-token control plane, bounded evidence promotion, typed outcomes, resumable orchestration, and private-data persistence boundaries have focused tests. A content-free loopback probe on 2026-08-14 returned HTTP 200 with a valid aggregate response. |
| G3 Tests | PENDING | The synthetic Research evaluation suite, v3 trusted evaluation/publish recomputation, v3 evidence-runner compatibility, UI smokes, and composed Research smoke pass. Task 14 still owns the complete release suite. |
| G4 Review | PENDING | No implementation review has occurred. |
| G5 Deployment health | PENDING | Nothing from this feature has been deployed. |
| G6 Online verification | PENDING | No production Research Run has been executed. |

## Current checkpoint

Tasks 1–12 are complete in the isolated `codex/research-agent-platform`
worktree. The platform now has durable Research Runs, privacy-bounded evidence,
the local Chatlog Worker protocol, role-separated orchestration, versioned v3
package policy, authenticated Research APIs, and the Chinese Research
workspace.

Task 13 adds the privacy-safe `research-agent-v1` synthetic suite. Its
deterministic gates cover retrieval scope, identity resolution and ambiguity,
timeline precision/recall, non-monotonic numeric trends, direct advice,
intervention/conflict extraction, case-transfer warnings, material-claim
citations, insufficiency, private projection, latency, and cost. Fabricated
recovery, ambiguous identity use, unsafe amount transfer, unsupported
conclusions, trend misclassification, and private projection are hard failures
that cannot be overridden by an aggregate score.

The trusted-suite path now accepts v3, stores the trusted-suite hash, recomputes
the report at persistence and publication time, and permits publication only
after the immutable evaluation passes. A v3 package that also declares an
evidence policy must pass both Research and evidence-audit hard gates before the
existing evidence runner accepts it. v1/v2 behavior remains on the existing
evaluation paths.

The repeatable process-level smoke starts an isolated KBase server, fake
Chatlog/TokenPlan loopback service, and real `chatlog-agent once` process. It
creates a deep run, waits with a bounded timeout, and verifies the Worker
heartbeat, completed job, promoted Chatlog evidence, searched/cited scope,
verified conclusion support, and durable event chain. The same smoke also runs
the deterministic coordinator restart/resume, Worker-offline,
identity-ambiguous, trusted-evaluation, and v3-publication cases. Process output
is checked for private sentinel leakage.

## Real-data acceptance checkpoint

An authorized, content-free probe confirmed that the real Chatlog service is
reachable on exact loopback and returns a valid aggregate response. No message,
identity, locator, or source content was printed or committed.

The complete real-data acceptance remains **PENDING** because this worktree has
no running Research-enabled KBase instance, no locally discoverable published
collection package, and no configured TokenPlan credential. Consequently no
real run ID or content hash is recorded, and G5/G6 remain pending. This is not
treated as a passing production proof; Task 14 must supply the reviewed clean
revision, selected immutable package, runtime credential, deployment, and
online verification.

No later gate may be marked PASS before its stated evidence exists, and a
failed gate returns to the responsible upstream task.
