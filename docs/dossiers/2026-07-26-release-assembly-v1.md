# Release Assembly v1 Delivery Dossier

## Status

Implementation and G3/G4 verification complete on
`codex/release-assembly-v1`. Clean-main integration and production rollout are
pending.

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

PASS on the final implementation candidate.

- Focused assembly, publication-identity, HTTP, contract, privacy, and
  cross-time-zone release-selection tests passed.
- `go test ./... -count=1` passed in the environment required by existing
  local-listener, network, and Keychain tests.
- `go test -race ./backend/app ./cmd/kbase-server -count=1` passed.
- `go vet ./...` and `go mod verify` passed.
- The Vue production build passed with only the repository's existing
  dependency `eval` and large-chunk warnings.
- Knowledge contract, evaluation, Proof consumer, system-map drift, privacy,
  Source Agent packaging, and WC Plus Agent packaging smokes passed.
- Every desktop and Web static UI smoke passed.
- The generated system map records `/api/knowledge/assembly` as authenticated.

### G4 - Review

PASS after remediation.

The initial independent review found four P2 issues:

- dangling claim citation IDs were not resolved against Release citations;
- internal file-system errors could reach the HTTP 500 body;
- changing the existing canonical publication key would break consumer
  identity stability;
- host-based Assembly identities were not opaque.

Commit `5a54b16` closed all four. Release evidence now fails on unresolved,
duplicate, or cross-book citation IDs; the HTTP route emits a generic internal
error; the existing readiness publication identity is unchanged; and Assembly
uses a separate fully opaque, transport-independent identity.

Independent re-review reported P1 PASS, P2 PASS, and G4 PASS.

Accepted P3 follow-ups:

- strengthen the runtime Assembly validator to enforce every JSON Schema
  relationship, not only the projection's required invariants;
- bound claims, statements, and potential-conflict references inside one
  cluster in addition to bounding the number of returned clusters.

### G5 - Deployment Health

Pending G3/G4 and clean-main integration.

### G6 - Online Verification

Pending authenticated metadata-only verification.

## Rollback

Remove the assembly projection, route, and schema, then restore the previous
canonical publication-identity behavior. No stored knowledge artifact requires
rollback because v1 is read-only.
