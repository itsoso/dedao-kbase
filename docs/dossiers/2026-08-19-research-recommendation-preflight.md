# Research Agent Recommendation Preflight Dossier

**Date:** 2026-08-19

**Status:** G1 PASS; G2-G6 pending

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
| G2 Feasibility and risk | PENDING | Domain, ranking, persistence, readiness, ownership, expiry, and drift contracts require focused implementation evidence. |
| G3 Tests | PENDING | Focused Go tests, race tests, HTTP/Web smokes, full Go tests, frontend build, system-map, privacy, and diff checks must pass. |
| G4 Review | PENDING | An independent review must find no unresolved release blocker in the exact candidate revision. |
| G5 Deployment health | PENDING | The exact reviewed revision must pass build, rollout, public and loopback health, service status, and rollback checks. |
| G6 Online verification | PENDING | The generic authorized collection case must complete through recommendation, confirmation, Run creation, and citation verification without exposing private data. |

No later gate may be marked PASS before its stated evidence exists. A failed
gate returns to the responsible upstream task.
