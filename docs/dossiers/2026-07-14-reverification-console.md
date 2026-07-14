# Knowledge Reverification Console Dossier

**Status:** Complete

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
- **G5 Deployment health:** PASS. PR #5 merged as `248cc6f`; the server-built
  binary SHA-256 is
  `1e92f09b037ef72955666404ebf39366caf745b93ba54364d6161961cc97ff54`.
  Binary and Web assets were replaced atomically, with backups at
  `/opt/dedao-kbase/bin/kbase-server.before-20260714185235` and
  `/opt/dedao-kbase/frontend-web.before-20260714185235`. The systemd unit is
  active and post-deployment logs contain no panic, fatal, or reverification
  error event.
- **G6 Online verification:** PASS. Public `/health` returned 200. Authenticated
  checks confirmed the deployed static bundle contains the review retry client,
  safe inline Markdown renderer, and Markdown answer styles; the target
  `source-2c403c4d3b68a4c4` package and book-scoped release listing are readable.
  A GET against the retry action returned 405 without mutating data. The
  behavior smoke verifies headings, bold text, inline code, separators, lists,
  safe links, and raw-script escaping. Browser screenshot automation could not
  reuse an authenticated session, so no credential bypass was attempted.

## Current Stage

S6 deployed and verified online.
