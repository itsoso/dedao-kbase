# Clinical Evidence Audit Agent Delivery Dossier

## Status

Implementation in progress on 2026-07-23.

## Requirement

Upgrade `book-agent-clinical-trials-truth` from a generic search/chat package
into a clinical-trial evidence audit product. The Agent must treat the book as
the primary thesis, compare its claims with pinned Dedao and WeChat knowledge
releases, produce a structured and cited audit, and prepare a bounded
Proofroom review task.

## Approved Scope

- Preserve versions `1.0.0` and `1.1.0` as immutable historical artifacts.
- Publish the incompatible workflow and report contract as `2.0.0`.
- Use KBase multi-source Releases; do not fetch mutable global content during
  an audit.
- Make structured evidence audit the primary product task and keep grounded
  chat secondary.
- Exclude diagnosis, treatment advice, write tools, and direct Health
  consumption from this release.

## Gate Decisions

### G1 - Admission

PASS. The current Agent is technically callable but does not yet satisfy the
product goal of turning a book into a complete vertical evidence product. Its
generic search/chat UI cannot express claim-level verdicts, conflict, evidence
independence, or Proofroom review work.

### G2 - Feasibility And Risk

PASS with controls.

- Existing immutable Knowledge Releases, Agent Packages, MCP tools, evaluation
  reports, and Agent Traces provide the required foundation.
- Multi-source runtime access remains restricted to Releases pinned at Package
  publication.
- The primary book cannot count as independent corroboration.
- Evidence-backed verdicts fail closed when citations do not resolve.
- Health remains isolated until a separate evidence-only medical review Gate.
- Real source bodies, credentials, and private evaluation inputs stay outside
  Git.

### G3 - Tests

Pending implementation.

### G4 - Review

Pending implementation.

### G5 - Deployment Health

Pending implementation.

### G6 - Online Verification

Pending implementation and private production evaluation.

## Design And Plan

- `docs/plans/2026-07-23-clinical-evidence-audit-agent-design.md`
- `docs/plans/2026-07-23-clinical-evidence-audit-agent.md`
