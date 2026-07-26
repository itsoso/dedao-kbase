# Citation Migration v1 Delivery Dossier

## Status

Implementation in progress on `codex/citation-migration-v1`.

## Requirement

Continue the approved Knowledge Foundation roadmap by making new knowledge
analysis use explicit citation IDs instead of legacy chunk references.

## Scope

- One citation per normalized HTML chunk.
- Citation-aware structured-analysis context.
- Strict citation allowlist for newly generated analysis.
- No automatic rewrite of existing packages or releases.

## Gate Decisions

### G1 - Admission

PASS. Phase 1 production readiness shows the evidence graph is operational,
but new analysis still defaults to legacy chunk references. Citation-native
generation is the smallest next end-to-end improvement.

### G2 - Feasibility And Risk

PASS with controls.

- Reuse the existing citation schema and evidence validator.
- Keep normal chat behavior unchanged.
- Advance the prompt version for provenance.
- Fail closed before quality evaluation.
- Preserve previous successful analysis on failure.
- Do not mutate the current corpus implicitly.

### G3 - Tests

Pending.

### G4 - Review

Pending.

### G5 - Deployment Health

Pending explicit clean-main deployment after G3/G4.

### G6 - Online Verification

Pending metadata-only production verification.

## Rollback

Revert the extractor, analysis context, prompt version, and strict generated
reference check. Existing packages and immutable releases require no data
rollback.
