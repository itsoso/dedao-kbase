# Clinical Evidence Audit Agent Design

## Goal

Upgrade `book-agent-clinical-trials-truth` from a generic book search/chat
surface into a reproducible clinical-trial evidence audit product. The Agent
must compare claims from the primary book with pinned Dedao and WeChat
knowledge releases, issue structured verdicts with resolvable citations, and
produce a review task that Proofroom can consume.

Versions `1.0.0` and `1.1.0` remain immutable. The incompatible workflow,
report contract, and primary UI task ship as `2.0.0`.

## Product Boundary

The primary user task is **Generate evidence audit**, not free-form chat. A user
selects existing claims from the primary book or enters an audit question. The
system returns:

- an executive conclusion;
- claim-by-claim `supported`, `contradicted`, `mixed`, or `insufficient`
  verdicts;
- a source-aware evidence matrix;
- limitations and knowledge gaps;
- requested review actions;
- a Proofroom-ready structured projection.

Free-form grounded conversation remains available as a secondary capability.
The Agent does not diagnose, prescribe, recommend treatment changes, purchase
content, or execute write tools.

## Architecture

The implementation extends the existing immutable Knowledge Release, Agent
Package, MCP, evaluation, and Trace layers.

1. **Package assembly** selects one primary book Release plus reviewed
   supporting Releases from Dedao courses and WeChat articles.
2. **Agent Package v2** pins every Release and declares an `EvidencePolicy`.
3. **EvidenceAuditRunner** executes deterministic stages: claim selection,
   cross-Release retrieval, citation resolution, evidence grouping, model
   adjudication, and report validation.
4. **EvidenceAuditStore** persists immutable input, status, report, hashes, and
   Trace references.
5. **Agent Web workspace** creates audits asynchronously, polls status, renders
   reports, and exposes stable audit URLs.
6. **Proofroom projection** maps a validated audit into a bounded review task.
   Proofroom retains multi-model review, human adjudication, and final trust
   scoring.

Runtime retrieval is restricted to Releases pinned by the Package. New or
changed source content requires a new Knowledge Release and Agent Package
version; an audit never searches mutable global content.

## Package Contract

`agent-package.v2` adds an evidence policy:

- primary Release IDs;
- supporting source roles;
- minimum independent supporting sources;
- maximum claims per audit;
- maximum evidence items per claim;
- allowed verdicts;
- freshness policy;
- report schema version.

The primary book does not count as independent verification. Package
publication fails when the policy references an unpinned Release, declares an
unsupported verdict, weakens the source usage policy, or lacks required
evaluation thresholds.

## Audit Contract

`evidence-audit.v1` contains:

- `audit_id`, status, timestamps, and idempotency/input hash;
- package identity, version, content hash, model route, and retrieval identity;
- immutable Release references and hashes;
- subject, scope, and selected source claims;
- claim audits with normalized statement, verdict, evidence, confidence,
  limitations, gaps, and review actions;
- report-level summary and Proofroom projection;
- output hash and Agent Trace ID.

Confidence is computed from citation completeness, independent source count,
source diversity, and conflicts. The model may provide rationale but cannot
set the final numeric confidence directly.

`supported`, `contradicted`, and `mixed` require at least one resolvable
citation. A claim with insufficient independent evidence must be downgraded to
`insufficient`. Every evidence item preserves Release, claim, chunk, citation,
source type, and publication identity.

## API And Execution

- `POST /api/agent-packages/{package_id}/audits?version={version}`
- `GET /api/agent-audits/{audit_id}`
- `GET /api/agent-packages/{package_id}/audits?version={version}`

Audit creation is asynchronous. The first release allows at most eight claims
and five evidence items per claim. Repeating an identical request returns the
existing audit instead of repeating model cost.

Each stage records explicit status. Model failure, unresolved citations,
Release hash mismatch, invalid output, or policy violation produces a failed
audit and failed Trace; partial output is never presented as a completed
report.

## Web Workspace

The Agent page presents:

1. package, Release, evaluation, and evidence-scope status;
2. a primary audit composer with claim selection and audit question;
3. running stage progress and recoverable errors;
4. conclusion, verdict counts, evidence matrix, conflicts, gaps, and review
   actions;
5. citation links and stable `/agents/.../audits/{audit_id}` history routes;
6. an explicit Proofroom preview/delivery action after validation;
7. grounded chat as a secondary section.

Desktop uses a report-first wide layout. Mobile keeps the audit composer and
report before package metadata, with no horizontal evidence-table overflow.

## Proofroom And Health

Proofroom receives structured claim, verdict, evidence-reference, limitation,
and requested-action data. It does not receive unrestricted source bodies.
Delivery is explicit, idempotent, and auditable.

Health does not consume this Agent in the first release. Medical-domain review,
freshness requirements, patient context, diagnosis, and treatment boundaries
remain owned by Health and require a separate evidence-only package.

## Evaluation And Release Gates

Tests cover:

- Package v2 and audit schema validation, canonical hashes, and immutability;
- supported, contradicted, mixed, insufficient, citation failure, source
  independence, model failure, idempotency, and cost limits;
- diagnosis/treatment abstention and write-tool blocking;
- retrieval recall and precision, faithfulness, citation resolution,
  adjudication consistency, abstention, latency, and cost;
- asynchronous API state, report history, stable routes, Markdown rendering,
  mobile layout, and error recovery;
- lossless Proofroom projection without automatic final adjudication.

Git fixtures use synthetic content. Private production evaluation data remains
outside the repository. Production publication requires a real multi-source
audit whose citations all resolve, followed by a Proofroom dry-run projection.

## Observability And Privacy

Traces record stage duration, package and Release hashes, retrieval hits,
citation resolution rate, independent source count, abstention reason, model
route, cost, and final outcome. They exclude credentials, downloaded source
bodies, private prompts, and consumer personal or health data.
