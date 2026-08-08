# Citation Migration v1 Delivery Dossier

## Status

COMPLETE. G1-G6 passed and production was verified on 2026-07-26.

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

PASS.

- Canonical `main` fast-forwarded to exact revision `57fff43`.
- Release archive SHA-256:
  `e033cbf3efd6af154bf73b5f9daa4b0c2943ef2485783ba028300ad2011ed16a`.
- Server preflight rebuilt both frontends, passed the full Go suite, knowledge
  contracts, evaluation, generated system map, privacy checks, and all static
  Web smoke checks before production mutation.
- The first preflight stopped on an unreachable default Go module proxy. The
  retry used the verified reachable mirror and did not bypass any test.
- The deployed Linux CGO binary SHA-256:
  `cec925d3bf523acce840b8f0205ff3505ecf732f14f21e59325f386d1b76a9c1`.
- Only the service binary and static Web bundle changed. Knowledge data,
  configuration, and secrets were preserved.
- Scoped backups were retained under rollback identifier
  `57fff43-20260726145202`.
- Post-deploy state was `active`, `ExecMainStatus=0`, and `NRestarts=0`.

### G6 - Online Verification

PASS.

- Public HTTPS health returned `{"ok":true,"service":"dedao-kbase"}`.
- The protected readiness route returned `401` without authorization and `200`
  through the service's normal authorization configuration.
- The metadata-only response used `knowledge_readiness.v1`, reported
  `total=266`, `ready=10`, `blocked=5`, and returned the requested maximum of
  three items.
- Recursive JSON inspection found no raw `source_account` field.
- The protected browser routes continued to require login.
- Recent service logs contained no `panic`, `fatal`, `error`, or `failed`
  entries, and the service remained healthy after all probes.

## Rollback

Revert the extractor, analysis context, prompt version, and strict generated
reference check. Existing packages and immutable releases require no data
rollback.
