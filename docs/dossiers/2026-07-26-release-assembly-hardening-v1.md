# Release Assembly Hardening v1 Delivery Dossier

## Status

Definition complete. Implementation pending.

## Requirement

Close the two accepted P3 findings from Release Assembly v1 before Agent
Compiler work begins.

## Gate Decisions

### G1 - Admission

PASS. The Assembly API is live, and the next consumer will be an Agent
Compiler. Relationship and payload-bound enforcement is cheaper before that
consumer contract exists.

### G2 - Feasibility And Risk

PASS with controls.

- Reuse deterministic derivation already used by the builder.
- Keep `knowledge_release_assembly.v1`.
- Reject instead of truncate.
- Do not mutate Releases or Assembly inputs.
- Keep production HTTP errors generic.

### G3 - Tests

Pending.

### G4 - Review

Pending.

### G5 - Deployment Health

Pending.

### G6 - Online Verification

Pending.

## Rollback

Restore the previous binary and static snapshot. No data rollback is required
because this change is read-only.

