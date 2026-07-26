# Release Assembly v1 Delivery Dossier

## Status

Deployed from canonical `main` at
`8f858abcee5779e5630ae95dae14246845ff8800`. G1-G6 are complete.

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

PASS after one blocked G6 cycle.

- Canonical `main` first advanced to `b8b4233`, then to compatibility revision
  `8f858ab`.
- The exact `8f858ab` archive SHA256 was
  `68f28770b511aa959d59777e9238ff36189a9423d53fd733ea0c9ae609aa3a5c`.
- The Linux preflight repeated the Vue build, all static UI smokes, full Go
  suite, vet, module verification, contract/evaluation/Proof consumer checks,
  system-map drift check, privacy check, and Source Agent packaging check.
- The installed binary SHA256 is
  `85db12b754b68632b7ef133f3c317b1a1ffad868c2791650eac98223042d52c3`.
- `dedao-kbase` is active with `NRestarts=0` and `ExecMainStatus=0`; local and
  public health probes return `{"ok":true,"service":"dedao-kbase"}`.
- Rollback snapshot: `8f858ab-20260726T094927Z`.

### G6 - Online Verification

PASS after remediation.

The first `b8b4233` production request correctly preserved the generic HTTP 500
body but exposed a legacy-data compatibility gap to the delivery gate. One
pre-contract Release lacked `schema_version`, and two historical analyses
referenced chunk IDs rather than citation IDs. No data was changed. Commit
`8f858ab` added an in-memory read adapter that:

- recognizes only version 1 Releases missing the later schema envelope;
- resolves legacy chunk references through the existing citation resolver;
- still rejects unknown, duplicate, and cross-book citations.

The fix was reproduced by a regression test and then executed read-only against
the production Release store before redeployment.

Authenticated verification through
`https://kbase.executor.life/api/knowledge/assembly?limit=3` returned:

- `schema_version=knowledge_release_assembly.v1`;
- `algorithm_version=deterministic-claim-assembly.v1`;
- assembly ID
  `assembly-4b54adc5c419ef23c2e460989bc9f1d0dd966e8764f5834371c3d877a3e1bb21`;
- 2 Releases, 15 claims, 15 clusters, and 3 returned clusters.

Recursive metadata checks found no raw account, prompt, answer, local path, or
host identity. The same public endpoint returns `401` without authorization.
Post-restart logs contain no panic, fatal error, or failed request.

## Rollback

Restore the binary and `frontend-web` from rollback snapshot
`8f858ab-20260726T094927Z`, then restart `dedao-kbase`. No stored knowledge
artifact requires rollback because v1 and its legacy adapter are read-only.
