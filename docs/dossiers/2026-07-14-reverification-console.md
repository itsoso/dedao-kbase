# Knowledge Reverification Console Dossier

**Status:** Ready for deployment

## Requirement

Continue the feedback-driven knowledge loop with an operator-facing review
console for asynchronous candidates.

## Scope

- Compact task status in the current book knowledge page.
- Candidate/release and quality evidence review.
- Book-scoped release lookup that is not truncated by global pagination.
- Safe manual retry for failed current tasks.
- Explicit, confirmed publication through the existing gate.

Out of scope: automatic publication, feedback deletion, raw model errors,
release mutation, and a separate administration application.

## Artifacts

- Design: `docs/plans/2026-07-14-reverification-console-design.md`
- Plan: `docs/plans/2026-07-14-reverification-console.md`

## Gates

- **G1 Admission:** PASS. Durable candidates need an operator action surface to
  complete the approved human publication loop.
- **G2 Feasibility and risk:** PASS WITH BOUNDARY. Existing release, quality,
  feedback, task, and publish APIs provide the evidence. Only failed-task retry
  is new; publication remains explicit and backend-gated.
- **G3 Test:** PASS. `go test ./...`, `go vet ./...`,
  `go test -race ./backend/app ./cmd/kbase-server`, `npm run build`, every
  `frontend-web/scripts/*.mjs` smoke check, `node --check frontend-web/app.js`,
  `bash scripts/privacy-smoke.sh`, and `git diff --check` passed. The frontend
  dependency audit still reports pre-existing vulnerabilities and large bundle
  warnings; no dependency or bundle architecture changed in this feature.
- **G4 Review:** PASS. Retry is serialized by the existing OS advisory lock,
  re-assesses the current invalidating feedback fingerprint, and rejects all
  non-failed or superseded tasks. The browser guards delayed responses by book
  identity, polls only active tasks, and requires both candidate and current
  quality decisions to pass before offering the already backend-gated publish
  action. No automatic publication path was added.
- **G5 Deployment health:** PENDING.
- **G6 Online verification:** PENDING.

## Current Stage

S5 implementation and local verification complete; awaiting deployment.
