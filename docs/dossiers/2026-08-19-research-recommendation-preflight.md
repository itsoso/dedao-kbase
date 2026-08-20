# Research Agent Recommendation Preflight Dossier

**Date:** 2026-08-19

**Status:** G1-G4 PASS; G5-G6 pending

**Approved design:**
[Research Agent Recommendation Preflight Design](../plans/2026-08-18-research-recommendation-preflight-design.md)

**Implementation plan:**
[Research Agent Recommendation Preflight Implementation Plan](../plans/2026-08-19-research-recommendation-preflight.md)

## Objective and approved boundaries

Add a deterministic preflight before Research Run creation. The service may
recommend and preselect up to three eligible v4 Agent Packages, but the operator
must confirm the final Package before a Run is created. Recommendation is not an
authorization to publish, upgrade, or otherwise mutate an Agent Package.

The first implementation is rules based. A model may later reorder only the
already eligible candidates; it may not widen eligibility or change readiness
decisions. When no eligible Package exists, the request is blocked and the
operator is guided to create or complete an Agent. The system must not fall back
to a global knowledge scope.

The feature keeps the existing shared Token and browser owner-scope mechanism.
It does not add signatures or a second authentication protocol.

## Privacy boundary

The public preflight result and committed artifacts must not contain question
bodies, evidence bodies, chat identities, message references, local paths,
locators, cookies, tokens, or downloaded source content. Durable preflight
records use a normalized request fingerprint and bounded safe summaries. Logs
may contain only the fingerprint, latency, candidate count, decision status,
and allowlisted error codes.

## Generic acceptance case

Use an authorized public-account collection Agent and a synthetic comparison
question that contains no personal or medical details.

Acceptance requires:

- the relevant collection Agent appears among at most three deterministic
  candidates with bounded reason codes;
- knowledge-only scope does not depend on a local chat Worker;
- adding local chat explicitly reports the current Worker readiness;
- a blocked readiness check prevents Run creation and gives an actionable next
  step;
- the operator confirms the Package before a Run is created;
- the completed Run retains verifiable citations within the selected immutable
  scope.

## Gate ledger

| Gate | Status | Evidence or exit condition |
| --- | --- | --- |
| G1 Admission | PASS | Human confirmation, deterministic recommendation, no-global-fallback, shared Token, privacy boundary, and generic acceptance case are frozen in the approved design and this dossier. |
| G2 Feasibility and risk | PASS | Bounded ranker/service counts, focused race outcome, process outcome, and owner/expiry rejection counts passed on the candidate recorded below. |
| G3 Tests | PASS | All focused, process, frontend, full Go, privacy, system-map, and diff outcomes recorded below exited zero. |
| G4 Review | PASS | Authoritative independent re-review outcome: READY; Critical `0`; Important `0`; Minor `0`. |
| G5 Deployment health | PENDING | The exact reviewed revision must pass build, rollout, public and loopback health, service status, and rollback checks. |
| G6 Online verification | PENDING | The generic authorized collection case must complete through recommendation, confirmation, Run creation, and citation verification without exposing private data. |

No later gate may be marked PASS before its stated evidence exists. A failed
gate returns to the responsible upstream task.

## G2/G3 candidate record

| Field | Value |
| --- | --- |
| Base revision | `cb94864c1857a7565bbee5d871288241b1b1fae8` |
| Ranker test SHA-256 | `5c5df272534a3cd8c95a905d52b3007bc590d3227f42caac5bbcdc1aa73ef8f7` |
| Service test SHA-256 | `583883d040dd558aba669efe927673718600e56a0839da42f1e559863dc54ba1` |
| Process smoke SHA-256 | `2e9c3f135a8a260217ed3051d1c025cd71d232389bc3a91c21aa045cdeeea50f` |
| Failure privacy test SHA-256 | `ac45abca03384e6b9cec038bcb7d6970791bb58828fc7c706f452d4a0ac420f2` |
| Binding mutation test SHA-256 | `aa2b588227233cf6b1f6a2c3f71162f40fe63f8e9c7d0a717c3df2d2fa0e5dd7` |
| Binding assertion SHA-256 | `cce425c9d1d6c9f640bd9a95301db334049e6b60d2f3a9c638876d33b6eaf96f` |
| Deterministic limits | catalog input `400`; published inputs `4`; artifact loads `4`; candidate output `3`; probe units `<=64` |
| Focused race | exit `0`; backend/app `8.286s`; kbase-server `2.998s`; elapsed `13.18s` |
| Process smoke | exit `0`; elapsed `46.08s`; standard self-tests `2`; exact binding checks `4`; rejection Run delta `0`; linked parent count `1`; citation re-fetch count `1` |
| Failure privacy and binding mutation | exit `0`; forbidden markers `6`; direct rejected binding mutations `8`; optimized modes `2`; optimized rejected binding mutations `16` |
| Web checks | syntax exit `0`; workspace smoke exit `0` |
| Frontend install/build | exit `0`; vulnerabilities `0`; build `2.79s`; engine/chunk advisories non-blocking |
| Go modules/vet | module verification exit `0`; vet exit `0` |
| Full Go test | exit `0`; current run cached; prior uncached backend/app `174.301s` |
| Privacy/system-map/diff | privacy exit `0`; system-map drift exit `0`; diff check exit `0` |
| Independent review | outcome `READY`; Critical `0`; Important `0`; Minor `0` |
