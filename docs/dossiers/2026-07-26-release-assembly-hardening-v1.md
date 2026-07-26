# Release Assembly Hardening v1 Delivery Dossier

## Status

G1-G4 complete on the final implementation candidate. Deployment pending.

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

Pending.

### G6 - Online Verification

Pending.

## Rollback

Restore the previous binary and static snapshot. No data rollback is required
because this change is read-only.
