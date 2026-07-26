# Release Assembly Hardening v1 Delivery Dossier

## Status

Deployed from canonical `main` at
`3379e490b4e598e48abc6a891cb5acd0aa47ad99`. G1-G6 are complete.

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

PASS on the final candidate.

- Bound and relationship tests cover oversized raw and resolved citations,
  statements, clusters, conflicts, forged counts, identities, status, edges,
  pagination, release membership, duplicate references, and input immutability.
- `go test ./... -count=1` passed.
- `go test -race ./backend/app ./cmd/kbase-server -count=1` passed.
- `go vet ./...` and `go mod verify` passed.
- The Vue production build passed with only the repository's existing
  dependency `eval` and large-chunk warnings.
- Knowledge contract, evaluation, Proof consumer, Source Agent, WC Plus Agent,
  system-map drift, and privacy smokes passed.
- Desktop and Web static UI smokes passed. No frontend source changed after
  those checks.

The first final-candidate contract smoke correctly blocked on a generated
system-map line-number drift introduced by the review fix. The map was
regenerated, its dedicated smoke passed, and the complete contract smoke then
passed.

### G4 - Review

PASS after remediation.

Structured review checked that limits run before quadratic conflict
derivation, runtime and schema limits agree, filtered summaries remain
consistent, validation does not mutate input, publication identities stay
opaque, and HTTP 500 responses remain generic.

One P2 issue was found: the builder counted citation IDs only after the legacy
read adapter had deduplicated them. A malformed Release containing more than
128 duplicate raw references could therefore bypass the input bound. A RED
regression reproduced the bypass. The builder now checks the raw list before
compatibility resolution and checks the resolved list again afterward. The
focused Assembly suite and full G3 gates passed after the fix.

### G5 - Deployment Health

PASS.

- Canonical `main` advanced by fast-forward to `3379e49`.
- The exact source archive SHA256 was
  `f62837a3cf9d51496d2bbf021fb0d2b98ec53c33369287d7e381546e89ad6568`.
- Linux preflight repeated dependency installation, Vue production build,
  every static UI smoke, the full Go suite, vet, module verification, contract,
  evaluation, Proof consumer, Source Agent packaging, system-map drift, and
  privacy checks.
- A read-only pre-deployment probe executed the final builder and validator
  against the production Release store without changing stored artifacts.
- The installed binary SHA256 is
  `0124efb0f68e0013b3e7ae1821e92fd5ea444aac2f2a8a5d051dc8c70fe0ab27`.
- `dedao-kbase` is active with `NRestarts=0` and `ExecMainStatus=0`; local and
  public health probes return `{"ok":true,"service":"dedao-kbase"}`.
- Rollback snapshot: `3379e49-20260726T115234Z`.

### G6 - Online Verification

PASS.

Authenticated production verification returned:

- `schema_version=knowledge_release_assembly.v1`;
- `algorithm_version=deterministic-claim-assembly.v1`;
- assembly ID
  `assembly-4b54adc5c419ef23c2e460989bc9f1d0dd966e8764f5834371c3d877a3e1bb21`;
- 2 Releases, 15 claims, 15 clusters, 3 returned clusters, and `has_more=true`.

A recursive verification independently checked response bounds, summary and
pagination relationships, claim membership and uniqueness, opaque publication
identities, and absence of raw account, prompt, answer, content, or local-path
data. The public API still returns `401` without authorization. Post-restart
logs contain no panic, fatal error, or failed request. Existing optional
integration warnings are unchanged and do not affect Assembly.

## Rollback

Restore the previous binary and static snapshot. No data rollback is required
because this change is read-only.
