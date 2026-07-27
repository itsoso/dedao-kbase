# Agent Compiler v1 PRD

## Objective

Turn an immutable Knowledge Release into a deterministic Agent Package
candidate without requiring operators to hand-author retrieval, model, prompt,
tool, safety, evaluation, evidence, or UI policy sections.

The compiler supports every-book study agents while preserving the stronger
multi-publication requirements needed by Proofroom and Health consumers.

## Users

- A reader creating a grounded study agent for one authorized book.
- A knowledge operator creating an evidence-only agent from a primary Release
  and independent supporting Releases.
- Proofroom and Health integrators consuming packages that passed existing
  trusted evaluation and publication gates.

## Product Modes

### Dual

Compile a study candidate and an evidence candidate in one response. A valid
study candidate remains ready when the evidence candidate is blocked.

### Evidence

Compile an `agent-package.v2` candidate. It requires one primary Release and at
least one independent supporting Release. Automatic support selection is
allowed only when Release Assembly proves a shared assertion or potential
conflict and the publication identity is independently eligible.

### Study

Compile an `agent-package.v1` candidate pinned to one Release. It supports
reading, search, grounded chat, and evidence inspection with read-only tools.

## Success Criteria

- The same normalized request and immutable Release state produce the same
  candidate content hash.
- Every ready candidate passes `ValidateAgentPackage`.
- A missing or unrelated supporting source produces bounded issue codes rather
  than a weak evidence package.
- Compiler output cannot publish or trust its own evaluation.
- The API requires the dedicated Agent publisher credential.
- Output contains no source body, prompt text, token, cookie, raw account, or
  machine-specific path.
- Operators can preview all three modes from the Book Agents Web workspace.

## Non-Goals

- Semantic similarity or LLM-based supporting-source selection.
- Automatic trusted-suite creation, evaluation approval, or publication.
- Write-capable tools, purchases, diagnosis, prescriptions, or personal-data
  mutation.
- Persisting draft candidates in v1; deterministic recompilation is the draft
  recovery mechanism.

## API

`POST /api/agent-packages/compile`

The request selects `dual`, `evidence`, or `study`, a primary Release, optional
supporting Releases, and a package version. The response uses
`agent-compilation.v1` and contains compiler identity, Assembly identity,
candidate status, packages when ready, bounded issues, and next actions.

## Rollout

Deliver the pure compiler and contract first, then publisher-authenticated HTTP
access, then the Web preview. Existing Agent Package evaluation, publication,
runtime, and consumer behavior must remain unchanged.
