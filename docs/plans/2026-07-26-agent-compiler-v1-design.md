# Agent Compiler v1 Design

## Context

KBase already owns immutable Releases, strict cross-Release Assembly, Agent
Package v1/v2 validation, trusted evaluation suites, publication gates,
runtime execution, and consumer delivery. The missing boundary is a safe,
repeatable translation from evidence state into package policy.

## Decision

Implement one pure, profile-driven compiler with three modes:

- `dual`: study plus evidence;
- `evidence`: evidence-only `agent-package.v2`;
- `study`: single-Release `agent-package.v1`.

Separate mode-specific endpoints were rejected because policy defaults would
drift. An asynchronous auto-publisher was rejected because compilation must not
approve the evaluation that protects publication.

## Contract

The input contract is `agent-compilation-request.v1`:

- `mode`;
- `primary_release_id`;
- optional `supporting_release_ids`;
- `version`.

The output contract is `agent-compilation.v1`:

- `compiler_version`;
- `mode`;
- `assembly_id`;
- normalized selected Release IDs;
- ordered candidate results;
- overall `ready`, `partial`, or `blocked` status.

Each candidate result has a stable kind, status, optional finalized package,
bounded issue codes, and next actions. Human-readable diagnostics are bounded
and are never used for program decisions.

## Deterministic Inputs

The compiler loads latest immutable Release state through the same compatibility
adapter used by Assembly, then validates the resulting package through
`ValidateAgentPackage`. Operational timestamps are omitted from candidates and
do not participate in package hashes.

Default package IDs are opaque and stable:

```text
book-agent-<sha256(book_id)[:16]>-study
book-agent-<sha256(book_id)[:16]>-evidence
```

The caller supplies only a version and Release selection. Arbitrary prompt,
model, tool, safety, or evaluation policy injection is not accepted.

## Study Profile

- `agent-package.v1`;
- one primary Release;
- lexical retrieval, citations required, bounded context;
- TokenPlan fallback model `qwen3.7-max`;
- grounded-answer prompt/output profile;
- all existing read-only Book MCP tools;
- standard usage policy and explicit abstention;
- reader, search, grounded chat, evidence, and quiz UI capabilities.

## Evidence Profile

- `agent-package.v2`;
- primary plus one or more supporting Releases;
- lexical retrieval and citation allowlists derived from Release claims;
- evidence-only usage and evidence-audit output;
- read-only Book MCP tools;
- existing evidence audit metrics and policy limits;
- reader, search, grounded chat, and evidence UI capabilities.

Explicit supporting Release IDs must belong to the same Assembly and pass
independent-publication checks. Automatic selection additionally requires a
cluster shared with the primary Release or a potential-conflict edge involving
both Releases. It never infers semantic similarity.

## Dual Semantics

Dual mode invokes both profiles over one loaded Assembly snapshot. The study
candidate may be `ready` while evidence is `blocked`; the response is then
`partial`. This prevents evidence scarcity from blocking the every-book study
goal without weakening the evidence contract.

## API And Authorization

Add `POST /api/agent-packages/compile`. It uses the dedicated Agent publisher
credential, a bounded body, strict unknown-field rejection, and generic
internal errors. Invalid request structure returns `400`; a valid request that
lacks support returns `200` with structured blocked candidates.

Compilation is read-only. Existing trusted-suite, evaluation, and publication
routes remain the only path to a runnable persisted package.

## Web Workspace

The Book Agents workspace gains a compact compiler panel:

- mode segmented control;
- primary and supporting Release selectors;
- package version;
- compile command;
- ready/blocked candidate summaries and next actions.

No publish shortcut is added. The result explains that trusted evaluation is
required before existing publication actions can succeed.

## Failure And Privacy Model

- Unknown, superseded, malformed, or non-Assembly Releases block compilation.
- Empty claim citation allowlists block the affected candidate.
- Duplicate selections and unsupported modes are rejected.
- Candidate and issue counts, strings, and request body size are bounded.
- No Release source body, raw source identity, prompt content, secret, or local
  path is returned.
- Compilation never mutates Releases, Assembly, evaluations, or package stores.

## Verification

Use RED/GREEN tests for deterministic hashes, all modes, partial dual output,
support selection, independent-source enforcement, package validation, input
immutability, publisher authentication, body bounds, generic internal errors,
privacy, and Web rendering. Run full Go, race, vet, module, frontend build,
contract, consumer, system-map, privacy, and static UI gates before deployment.
