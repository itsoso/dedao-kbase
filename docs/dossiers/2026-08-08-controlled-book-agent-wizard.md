# Controlled Book Agent Wizard Dossier

## Status

Implementation complete; full verification, deployment, and production
acceptance pending.

## Requirement

Provide a human-controlled path from one authorized Dedao ebook to a usable
knowledge base and Book Agent. Preserve the shared consumer token for ordinary
browser/API access, keep the dedicated Agent Package publisher credential on the
server, and prevent quality or evaluation holds from being bypassed.

## Definition Gate (G1)

Decision: PASS.

- Scope is one existing authorized ebook and the reusable platform path.
- The first Agent is read-only and supports reading, search, grounded chat, and
  evidence inspection.
- Autonomous publication, write tools, vector fallback, and browser publisher
  credentials are excluded.

## Feasibility And Risk Gate (G2)

Decision: PASS with explicit proxy requirement.

- Existing immutable Knowledge Release, Agent Package, evaluation, runtime, and
  publication contracts are reused.
- A legacy Dedao fallback package can contain complete retrieval objects while
  lacking `content_hash`; this deterministically blocks quality publication.
- The controlled browser routes require both normal API authorization and a
  constant-time verified proxy marker matching `KBASE_BROWSER_SESSION_SECRET`.
  The proxy must clear client-supplied markers on general API routes.

## Implementation

### Content identity

- Canonical SHA-256 identity covers stable book metadata, chapters, chunks,
  claims, and citations.
- Collection order and volatile timestamps do not alter identity.
- New packages receive an identity before persistence.
- Legacy repair applies only to an empty identity and invalidates stale analysis
  and quality artifacts before saving the repaired package.

### Initial knowledge release

- The review workspace exposes legacy repair when required.
- Analysis completion refreshes the quality report automatically.
- A current passing quality report enables the first immutable Knowledge
  Release action.

### Controlled Agent

- The server derives a contract-valid lexical, citation-required, read-only
  Agent Package from one published Knowledge Release.
- It generates a ten-metric deterministic suite covering retrieval recall and
  precision, citations, faithfulness, abstention, tool selection and arguments,
  task completion, latency, and cost.
- Draft, evaluation, and publication are separate explicit browser actions.
- Publication requires a passing persisted evaluation, explicit confirmation,
  a stable idempotency key, trusted browser-session provenance, and configured
  server-side publisher authority.

## Test Gate (G3)

Pending full-suite verification. Focused RED/GREEN evidence:

- canonical content-hash tests;
- missing-hash persistence and legacy repair tests;
- initial release Web smoke checks;
- generated Agent Package contract and deterministic evaluation tests;
- controlled browser-session, evaluation, confirmation, publication, and
  credential non-disclosure HTTP tests;
- three-step wizard Web smoke checks.

## Review Gate (G4)

Pending final diff and privacy review.

## Deployment Health Gate (G5)

Pending deployment. Required checks:

- clean verified build artifact;
- active service with zero new restarts;
- public health success;
- general API requests cannot forge the trusted browser marker;
- Basic-Auth browser path can use the controlled workflow.

## Online Verification Gate (G6)

Pending. Acceptance sequence for the pilot book:

1. repair the empty content identity;
2. regenerate analysis and obtain a passing quality report;
3. publish one immutable Knowledge Release;
4. generate and evaluate the controlled Agent draft;
5. explicitly publish the Agent Package;
6. verify grounded search/chat citations and Book App links;
7. verify no response or stored browser state exposes publisher authority.

No Gate may advance with a failed quality, evaluation, privacy, deployment, or
online acceptance check.
