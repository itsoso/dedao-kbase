# Health Evidence Review Workspace v2 Dossier

**Status:** IMPLEMENTED — G3/G4 PASS locally; push/deploy pending

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

## G5/G6

Not attempted. Deployment requires push/integration and must happen only from a
clean main branch after the user authorizes the release path.
