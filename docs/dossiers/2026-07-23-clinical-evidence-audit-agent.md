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

## Implementation Checkpoint

### Task 1 - Agent Package v2 Evidence Policy

Completed and independently reviewed. The immutable Package contract pins the
primary and supporting Releases, verdict policy, freshness, evidence limits,
and report schema.

### Task 2 - Immutable Evidence Audit Store

Completed and independently reviewed. Inputs and reports are
content-addressed, terminal reports are immutable, cross-process writes are
locked, and crash recovery uses bounded journals and last-known-good manifests.

### Task 3 - Multi-source Evidence Audit Runner

Completed and independently reviewed. The runner enforces Package-scoped
retrieval, citation resolution, source independence, freshness, verdict and
cost policy, deterministic medical abstention, model checkpoints, and durable
Audit/Trace terminal coordination.

### Task 4 - Durable Coordinator And HTTP API

Completed, hardened, and verified.

- The coordinator uses the persistent Audit store as the queue of record.
  In-memory queues carry Audit IDs only; a worker atomically claims the
  cross-process lease immediately before execution and starts heartbeats at
  once. Competing instances may observe and enqueue the same durable task, but
  non-owners emit a structured skip and never execute it. This prevents queued
  work from holding an expiring lease or starving behind a long-running Audit.
  Expired leases can still be claimed after a crashed worker, while a busy
  execution lock never fails another owner's Audit.
- Recovery scans use a bounded cursor page instead of reading all Audit
  records each second. The cursor advances only through work successfully
  processed by the local queue, so queue pressure cannot skip or starve later
  records. Scan, claim, renew, release, and execution failures use bounded
  exponential backoff with injectable jitter and structured metric/log events.
- Authenticated asynchronous create, list, detail, and explicit manual retry
  endpoints expose the workflow without automatically retrying failed Audits.
- Every Audit API failure, including authentication, method errors, missing
  Packages, and storage failures, returns a stable `{code,error}` response.
  Full internal diagnostics go only to the injected server logger after
  case-insensitive redaction of bearer/basic credentials, API keys, secrets,
  passwords, sessions, CSRF values, and access/refresh tokens in JSON, query,
  and header forms.
- Retry authorization is derived from the authenticated actor and signed with
  a server-side HMAC key. Bearer credentials and signing keys are not persisted.
- Missing TokenPlan configuration leaves the service online but makes Audit
  creation return a diagnostic service-unavailable response.
- The HTTP server has fail-closed environment parsing and safe defaults for
  header, read, write, and idle timeouts plus maximum header size.
- Focused and race-enabled coordinator/API tests pass without data races.
- The complete backend and server command suites, `go vet`, privacy and system
  map smoke checks, diff checks, and Linux, Windows, and macOS compile checks
  pass.
