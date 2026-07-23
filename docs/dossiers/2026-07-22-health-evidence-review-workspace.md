# Health Evidence Review Workspace v2 Dossier

**Status:** COMPLETE — G1-G6 PASS; production KBase deployed and verified

## S0 · User request

Continue product improvements after the Knowledge Operations Console first
release. Proceed with the next useful improvement without waiting for routine
approval.

Chosen slice: a read-only Health Evidence Review Workspace v2 queue inside the
Knowledge Operations Console.

Required boundaries:

- no automatic Health serving promotion;
- no source body exposure;
- no unsafe replay;
- no Health consumer review or safety ownership inside KBase;
- stop on failed Gate, genuine blocker, destructive action, deployment before
  G3/G4, or required human/secret decision.

## S1 · Discovery

- Repository `AGENTS.md` and `docs/system-map/INDEX.md` were read.
- Existing operations console has authenticated API and `/operations` frontend
  route.
- Existing operations items include per-package Health summary, counts, reasons,
  safe replay, and dangerous action rejection.
- Gap: there is no dedicated Health review queue sorted by operator priority.
- Work continues in clean worktree branch
  `codex/health-review-workspace-v2` based on `dedao-kbase/main`.

## G1 · Admission

- first_class_objects: Health review queue item, priority label, next operator
  action, consumer review required marker.
- core_loop_step: observe Health evidence candidates -> prioritize review queue
  -> guide human/operator next action without mutating Health serving state.
- smallest_end_to_end_slice: one authenticated operations response and
  `/operations` panel showing queue items with metadata only.
- risk: medium because health-related evidence metadata is involved.
- decision: PASS. Scope is read-only visibility and preserves consumer
  ownership.

## S2 · Product/design

Design document:
`docs/plans/2026-07-22-health-evidence-review-workspace-design.md`.

Decision: extend KBase Operations Console with a derived Health review queue,
not a Health review mutation or serving promotion workflow.

## S3 · Plan

Implementation plan:
`docs/plans/2026-07-22-health-evidence-review-workspace.md`.

## G2 · Feasibility and safety

- Reuses existing KBase package, release, quality, pipeline, and Health
  readiness metadata.
- Adds no new source adapter, external download, or secret.
- Returns metadata, counts, risk distribution, reasons, and action labels only.
- Does not serialize source bodies, downloaded article text, or claim
  statements.
- Keeps safe replay allowlist unchanged: `analyze` and `evaluate_quality` only.
- decision: PASS.

## Checkpoints

### Task 1

- Created lifecycle design, implementation plan, and dossier.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.

### Task 2 · Backend queue contract

- RED:
  `GOCACHE=/private/tmp/dedao-kbase-health-review-v2/.codex-go-cache go test ./backend/app -run TestKnowledgeOperationsBuildsHealthReviewQueue -count=1`
  failed with missing `HealthReviewQueue` and
  `KnowledgeOperationsHealthReviewItem`.
- GREEN:
  `GOCACHE=/private/tmp/dedao-kbase-health-review-v2/.codex-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-health-review-v2/.codex-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-health-review-v2/.codex-kbase-test go test ./backend/app -run TestKnowledgeOperationsBuildsHealthReviewQueue -count=1`
  — PASS.
- Added `health_review_queue` to `knowledge_operations.v1`.
- Queue is derived from existing operations items and sorted by deterministic
  priority.

### Task 3 · Backend privacy and safety regression

- `GOCACHE=/private/tmp/dedao-kbase-health-review-v2/.codex-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-health-review-v2/.codex-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-health-review-v2/.codex-kbase-test go test ./backend/app -run 'KnowledgeOperations.*Health|KnowledgeOperationsBuildsHealthReviewQueue|RunKnowledgeOperationsReplayRejectsDangerousActions' -count=1`
  — PASS.
- Regression confirms queue JSON does not include Health claim statements.
- Queue item `serving_allowed` is always `false`.
- Queue next-operator action labels do not use `publish` or
  `health_serving_promote`.

### Task 4 · Frontend queue workspace

- RED: `node frontend-web/scripts/knowledge-operations-console-smoke.mjs`
  failed with `app.js should include health_review_queue`.
- GREEN:
  `node --check frontend-web/app.js && node frontend-web/scripts/knowledge-operations-console-smoke.mjs`
  — PASS.
- Added a read-only Health Evidence Review Queue panel to `/operations`.
- Panel shows priority, status, next operator action, review requirement,
  serving flag, claim/citation counts, risk counts, and reasons.

### Task 5 · System-map and verification

- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json`
  — PASS with explicit temporary Go cache/config environment.
- `env GOCACHE=/private/tmp/dedao-kbase-health-review-v2/.codex-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-health-review-v2/.codex-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-health-review-v2/.codex-kbase-test bash scripts/system-map-smoke.sh`
  — PASS.
- `GOCACHE=/private/tmp/dedao-kbase-health-review-v2-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-health-review-v2-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-health-review-v2-store go test ./backend/app -run 'KnowledgeOperations' -count=1`
  — PASS.
- `node --check frontend-web/app.js` — PASS.
- `node frontend-web/scripts/knowledge-operations-console-smoke.mjs` — PASS.
- `npm install` — PASS; npm reported existing audit findings:
  1 low, 6 moderate, 9 high.
- `npm run build` from `frontend/` — PASS; Vite reported existing eval/chunk
  size warnings.
- First sandboxed
  `go test ./... -timeout=180s` failed because the sandbox denied local
  `httptest` port binding, DNS, and macOS keychain access.
- Non-sandbox rerun:
  `GOCACHE=/private/tmp/dedao-kbase-health-review-v2-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-health-review-v2-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-health-review-v2-store go test ./... -timeout=180s`
  — PASS.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.

## G3 · Test Gate

- Focused backend Operations tests passed.
- Frontend-web syntax and operations smoke passed.
- System-map drift check passed after regeneration.
- Full Go test suite passed in non-sandbox execution after frontend build.
- Privacy smoke and whitespace checks passed.

Decision: PASS.

## G4 · Safety Review Gate

- New queue is read-only and derived from existing KBase metadata.
- No new mutating route or downstream Health write was added.
- Health serving remains outside KBase; every queue item reports
  `serving_allowed=false`.
- Queue actions are operator guidance labels, not replay commands.
- Safe replay allowlist remains unchanged.
- Tests cover no claim statement exposure in the queue.
- No source bodies, downloaded content, prompts, tokens, cookies, or secrets were
  added to fixtures, docs, or UI.

Decision: PASS.

## Main integration and deployment authorization

- User requested deployment after the feature branch was pushed.
- Deployment did not proceed from the feature branch because remote
  `dedao-kbase/main` had advanced to
  `6f35cbb097ff2a687387d3d4373dc0b0361a60d0`.
- Created clean integration worktree branch
  `deploy/health-review-workspace-v2` from `dedao-kbase/main`.
- Merged `dedao-kbase/codex/health-review-workspace-v2` with no conflicts:
  `0a4adbb feat(kbase): merge health review queue`.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- Initial merged-main `bash scripts/system-map-smoke.sh` failed because
  concurrent main changes moved generated source line numbers.
- Regenerated `docs/_generated/system-map.json`; `bash scripts/system-map-smoke.sh`
  — PASS.
- Committed the generated map sync:
  `93ba05a docs(kbase): refresh deploy system map`.
- `npm install` in the clean main worktree — PASS; npm reported existing audit
  findings: 1 low, 6 moderate, 9 high.
- `npm run build` from `frontend/` — PASS; Vite reported existing eval/chunk
  size warnings.
- `go test ./backend/app -run 'KnowledgeOperations' -count=1` — PASS.
- `node --check frontend-web/app.js` — PASS.
- `node frontend-web/scripts/knowledge-operations-console-smoke.mjs` — PASS.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- Sandboxed `go test ./... -timeout=180s` failed on environment restrictions:
  local `httptest` port binding, DNS, and macOS keychain access.
- Non-sandbox rerun:
  `go test ./... -timeout=180s` — PASS.
- `git push dedao-kbase HEAD:main` fast-forwarded main from `6f35cbb` to
  `93ba05a`.

## Deployment cache-bust hardening

- Pre-deploy static check found `frontend-web/index.html` still referenced
  `20260721-audio-detail` while this release changed `/app.js` and
  `/styles.css`.
- RED: `node frontend-web/scripts/knowledge-operations-console-smoke.mjs`
  failed with `index.html should bust cache for health review queue assets`.
- Updated `frontend-web/index.html` to
  `20260722-health-review-queue`.
- `book-knowledge-web-smoke` then failed because it still expected the previous
  cache version. Updated it to the new release version.
- GREEN:
  `set -e; node frontend-web/scripts/knowledge-operations-console-smoke.mjs; node frontend-web/scripts/book-knowledge-web-smoke.mjs; node --check frontend-web/app.js; bash scripts/privacy-smoke.sh; git diff --check`
  — PASS.
- Committed:
  `7dfe2a8 fix(kbase): refresh health review assets`.
- `git push dedao-kbase HEAD:main` fast-forwarded main from `93ba05a` to
  `7dfe2a8`.

## G5 · Deployment Health Gate

Decision: PASS.

- Deployed exact clean-main revision:
  `7dfe2a884e0ab51c9883a793c3381cee797d48ca`.
- Local release archive:
  `/private/tmp/kbase-release-7dfe2a8.tar.gz`.
- Release archive SHA-256:
  `4721db3143031ada08b389e6eeda08d3d1dd50554ab1df927e1c0449ac30a019`.
- Production preflight before rollout:
  `systemctl is-active dedao-kbase` — `active`;
  `ExecMainStatus=0`; `NRestarts=0`;
  local `/health` returned `{"ok":true,"service":"dedao-kbase"}`;
  `/opt/go-toolchains/go1.23.0/bin/go version` returned
  `go version go1.23.0 linux/amd64`.
- Server-side release preflight in `/tmp/kbase-release-7dfe2a8`:
  `node --check frontend-web/app.js` — PASS;
  all `frontend-web/scripts/*smoke*.mjs` — PASS;
  `/opt/go-toolchains/go1.23.0/bin/go test ./... -timeout=180s` — PASS.
- Server-side Linux CGO build:
  `CGO_ENABLED=1 /opt/go-toolchains/go1.23.0/bin/go build -trimpath -o /tmp/kbase-server-7dfe2a8 ./cmd/kbase-server`
  — PASS.
- Production binary SHA-256:
  `b52d9e85ed68943bbdc235c5365bf89734dafe6c99f90e3d8e7bbc5ed1549b4a`.
- Deployment replaced only:
  `/opt/dedao-kbase/bin/kbase-server` and
  `/opt/dedao-kbase/frontend-web`.
- KBase data, artifact, and secret directories were not touched.
- Backups:
  `/opt/dedao-kbase/bin/kbase-server.backup-7dfe2a8-20260723112116`;
  `/opt/dedao-kbase/frontend-web.backup-7dfe2a8-20260723112116`.
- Post-rollout checks:
  deployed binary hash matched
  `b52d9e85ed68943bbdc235c5365bf89734dafe6c99f90e3d8e7bbc5ed1549b4a`;
  `systemctl is-active dedao-kbase` — `active`;
  `ExecMainStatus=0`; `NRestarts=0`;
  local `/health` returned `{"ok":true,"service":"dedao-kbase"}`.

## G6 · Online Verification Gate

Decision: PASS.

- Public `https://kbase.executor.life/health` returned
  `{"ok":true,"service":"dedao-kbase"}`.
- Public `https://health.executor.life/api/v1/health` returned healthy with
  API, database, Redis, and Celery connected.
- Local deployed `/operations` references
  `20260722-health-review-queue`.
- Local deployed `/app.js` contains `health_review_queue` and
  `Health Evidence Review Queue`.
- Local deployed `/styles.css` contains `knowledge-operations__queue`.
- Authenticated production
  `GET /api/knowledge/operations?limit=5` returned
  `schema=knowledge_operations.v1`, `total=5`, `items=5`, and
  `queue=0`.
- Authenticated dangerous replay probe with action `publish` returned HTTP
  `409` and contained `not allowed`.
- Authenticated safe replay planning probe with action `evaluate_quality`
  returned HTTP `200`, `status=planned`, and `mutated=false`.
- `systemctl is-active dedao-kbase` remained `active`;
  `ExecMainStatus=0`; `NRestarts=0`.
- `journalctl -u dedao-kbase --since "5 minutes ago"` contained no
  `panic|fatal|error|failed` lines.

## Push

- Feature implementation commit:
  `4d8e202 feat(kbase): add health review queue`.
- Before push:
  `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- `git status --short` — clean.
- `git push -u dedao-kbase codex/health-review-workspace-v2` — PASS.
- Remote branch:
  `dedao-kbase/codex/health-review-workspace-v2`.
- Pull request URL suggested by GitHub:
  `https://github.com/itsoso/dedao-kbase/pull/new/codex/health-review-workspace-v2`.
