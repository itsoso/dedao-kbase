# Citation Migration v1 Delivery Dossier

## Status

Ready for clean-main deployment from `codex/citation-migration-v1`.

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

PASS.

- `go test ./... -count=1`
- `go test -race ./backend/app ./cmd/kbase-server -count=1`
- `go vet ./...`
- `go mod verify`
- `npm run build` in `frontend/`
- Knowledge contract, evaluation, system-map, privacy, Markdown, book UI, and
  WC Plus UI smoke checks
- Focused regression tests for citation identity, exposed-source allowlisting,
  legacy-body omission, prompt migration, and invalid-reference redaction

### G4 - Review

PASS after three independent review rounds.

The first two reviews blocked delivery until analysis validation was limited to
citations actually present in the model context, legacy chunk bodies were
removed from citation-native context, risks and actions were fully validated,
and invalid model references were made opaque. The final review found no P1 or
P2 issues.

Accepted P3 follow-up: completely identical chapters with the same title and
body cannot have both unique and insertion-stable citation IDs without a stable
upstream section identifier. Current identity is stable across ordinary
earlier insertions and same-body chapters with distinct titles.

### G5 - Deployment Health

Pending explicit clean-main deployment after G3/G4.

### G6 - Online Verification

Pending metadata-only production verification.

## Rollback

Revert the extractor, analysis context, prompt version, and strict generated
reference check. Existing packages and immutable releases require no data
rollback.
