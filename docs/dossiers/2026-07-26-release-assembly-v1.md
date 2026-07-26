# Release Assembly v1 Delivery Dossier

## Status

Implementation in progress on `codex/release-assembly-v1`.

## Requirement

Continue the Knowledge Foundation roadmap by adding a deterministic
cross-Release assembly layer before Agent Package compilation.

## Scope

- Latest immutable Release per book.
- Deterministic exact-assertion and explicit-negation clustering.
- Conservative potential-conflict candidates.
- Canonical independent-publication counts.
- Authenticated privacy-safe read-only API.

## Gate Decisions

### G1 - Admission

PASS. Citation-native Releases are now available, but Agent builders still need
manual cross-book comparison. A deterministic assembly projection is the
smallest shared improvement for Agent compilation and Evidence Audit.

### G2 - Feasibility And Risk

PASS with controls.

- No LLM or embedding call in v1.
- No paraphrase equivalence claim.
- Potential conflicts are review candidates, not contradiction verdicts.
- Invalid selected Releases fail the complete projection.
- Existing Releases, packages, and audits remain immutable.
- Output contains bounded metadata and claim statements only.

### G3 - Tests

Pending.

### G4 - Review

Pending.

### G5 - Deployment Health

Pending G3/G4 and clean-main integration.

### G6 - Online Verification

Pending authenticated metadata-only verification.

## Rollback

Remove the assembly projection, route, and schema, then restore the previous
canonical publication-identity behavior. No stored knowledge artifact requires
rollback because v1 is read-only.

