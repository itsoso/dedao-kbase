# Agent Compiler v1 Delivery Dossier

## Status

Definition complete. Implementation pending.

## Requirement

Compile deterministic study and evidence Agent Package candidates from strict
Release Assembly state, in dual, evidence, and study modes.

## Gate Decisions

### G1 - Admission

PASS. Package validation, evaluation, publication, and runtime already exist.
Manual package policy authoring is now the primary gap between Releases and
per-book Agents.

### G2 - Feasibility And Risk

PASS with controls.

- Pure read-only compiler.
- Fixed policy profiles rather than arbitrary policy input.
- No automatic trusted evaluation or publication.
- Evidence mode fails closed without independent support.
- Existing package schemas and runtime remain authoritative.

### G3 - Tests

Pending.

### G4 - Review

Pending.

### G5 - Deployment Health

Pending.

### G6 - Online Verification

Pending.

## Rollback

Remove the compiler route and Web panel, then restore the prior binary and
static snapshot. No stored artifact rollback is required because compilation is
read-only.
