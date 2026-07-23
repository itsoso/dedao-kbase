# Operations Empty-State Diagnostics Dossier

**Status:** DEPLOYED — G1/G2/G3/G4/G5/G6 PASS

## S0 · User request

Continue optimizing the Knowledge Operations Console. The selected direction is
Option A: queue empty-state diagnostics and safe next steps for the Health
review queue.

Required boundaries:

- no automatic Health serving promotion;
- no KBase-owned Health review or safety decision;
- no source body or claim statement exposure;
- no unsafe replay;
- stop on failed Gate, genuine blocker, destructive action, deployment before
  G3/G4, or required human/secret decision.

## S1 · Discovery

- Repository `AGENTS.md` and `docs/system-map/INDEX.md` were read.
- Current production Operations Console v2 is complete and deployed.
- G6 for v2 observed `health_review_queue` returning `queue=0` on the
  production `limit=5` operations response.
- Existing frontend shows an empty queue message, but does not explain whether
  the empty state means no data, current limit mismatch, upstream work, or
  downstream Health-owned review/import.
- Work starts in clean feature worktree branch
  `codex/operations-diagnostics-v3` based on `dedao-kbase/main`.

## G1 · Admission

- first_class_objects: Health review diagnostics, queue empty reason, status
  counts, blocker summary, next safe action.
- core_loop_step: observe empty or blocked review queue -> explain cause ->
  suggest safe operator next step.
- smallest_end_to_end_slice: authenticated operations response plus
  `/operations` panel explaining empty queue state.
- risk: medium because Health review metadata is involved.
- decision: PASS. Scope is read-only operations visibility and preserves
  consumer ownership.

## S2 · Product/design

Design document:
`docs/plans/2026-07-23-operations-empty-state-diagnostics-design.md`.

Decision: derive diagnostics from existing KBase operations metadata and render
them as explanation, not commands.

## S3 · Plan

Implementation plan:
`docs/plans/2026-07-23-operations-empty-state-diagnostics.md`.

## G2 · Feasibility and safety

- Reuses current operations items, Health readiness status, queue state, and
  summary totals.
- Adds no new route, token, source adapter, external download, or downstream
  Health write.
- Returns counts and labels only.
- Safe next actions are guidance strings, not replay commands.
- decision: PASS.

## Checkpoints

### Task 1

- Created lifecycle design, implementation plan, and dossier.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.

### Task 2 · Backend diagnostics contract

- RED:
  `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store go test ./backend/app -run TestKnowledgeOperationsExplainsEmptyHealthReviewQueue -count=1`
  failed with missing `HealthReviewDiagnostics` and
  `buildKnowledgeOperationsHealthReviewDiagnostics`.
- GREEN:
  `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store go test ./backend/app -run TestKnowledgeOperationsExplainsEmptyHealthReviewQueue -count=1`
  — PASS.
- Added `health_review_diagnostics` to `knowledge_operations.v1`.
- Diagnostics include `queue_empty_reason`, `status_counts`,
  `next_safe_actions`, and `blockers`.

### Task 3 · Backend privacy and safety regression

- `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store go test ./backend/app -run 'KnowledgeOperations.*Health|KnowledgeOperationsExplainsEmptyHealthReviewQueue|RunKnowledgeOperationsReplayRejectsDangerousActions' -count=1`
  — PASS.
- Diagnostics safe-action labels avoid `publish` and
  `health_serving_promote`.

### Task 4 · Frontend diagnostics panel

- RED: `node frontend-web/scripts/knowledge-operations-console-smoke.mjs`
  failed with `app.js should include health_review_diagnostics`.
- GREEN:
  `node --check frontend-web/app.js && node frontend-web/scripts/knowledge-operations-console-smoke.mjs && node frontend-web/scripts/book-knowledge-web-smoke.mjs`
  — PASS.
- Added Health Queue Diagnostics panel under the review queue.
- Updated frontend-web cache-bust version to
  `20260723-operations-diagnostics`.

### Task 5 · Verification

- `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store go run ./cmd/system-map --root . --out docs/_generated/system-map.json`
  — PASS.
- `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store bash scripts/system-map-smoke.sh`
  — PASS.
- `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store go test ./backend/app -run 'KnowledgeOperations' -count=1`
  — PASS.
- `node --check frontend-web/app.js` — PASS.
- `node frontend-web/scripts/knowledge-operations-console-smoke.mjs` — PASS.
- `node frontend-web/scripts/book-knowledge-web-smoke.mjs` — PASS.
- `npm install` — PASS; npm reported existing audit findings:
  1 low, 6 moderate, 9 high.
- `npm run build` from `frontend/` — PASS; Vite reported existing eval/chunk
  size warnings.
- Sandboxed `go test ./... -timeout=180s` failed because the sandbox denied
  local `httptest` port binding, DNS, and macOS keychain access.
- Non-sandbox rerun:
  `GOCACHE=/private/tmp/dedao-kbase-operations-v3-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-v3-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-v3-store go test ./... -timeout=180s`
  — PASS.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.

## G3 · Test Gate

- Focused backend Operations tests passed.
- Frontend-web syntax and operations/book smoke passed.
- System-map drift check passed after regeneration.
- Full Go suite passed in non-sandbox execution after frontend build.
- Privacy smoke and whitespace checks passed immediately before commit.

Decision: PASS.

## Push

- Feature commit:
  `fe10760 feat(kbase): explain operations queue empty state`.
- Before push:
  `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- `git status --short` — clean.
- `git push -u dedao-kbase codex/operations-diagnostics-v3` — PASS.
- Remote branch:
  `dedao-kbase/codex/operations-diagnostics-v3`.
- Pull request URL suggested by GitHub:
  `https://github.com/itsoso/dedao-kbase/pull/new/codex/operations-diagnostics-v3`.

## G5/G6

### Main integration and deploy authorization

- Deploy worktree:
  `/private/tmp/dedao-kbase-operations-diagnostics-deploy`.
- Base:
  `dedao-kbase/main` at
  `ef0cce50e3e0a35100317555ff4d7d89fba4204c`.
- Feature:
  `dedao-kbase/codex/operations-diagnostics-v3` at
  `ca3b893bbf3c8949c58ade374bf702eba431d5ef`.
- Merge commit:
  `abc1d67e5dec0d377b5e955a4fc21ec07cadeb28`
  (`feat(kbase): merge operations diagnostics`).
- User requested deployment with `部署`.
- `git push dedao-kbase HEAD:main` — PASS:
  `ef0cce5..abc1d67 HEAD -> main`.

### G3/G4 rerun on main integration revision

- `GOCACHE=/private/tmp/dedao-kbase-operations-diagnostics-deploy-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-diagnostics-deploy-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-diagnostics-deploy-store bash scripts/system-map-smoke.sh`
  — PASS.
- `GOCACHE=/private/tmp/dedao-kbase-operations-diagnostics-deploy-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-diagnostics-deploy-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-diagnostics-deploy-store go test ./backend/app -run 'KnowledgeOperations' -count=1`
  — PASS.
- `node --check frontend-web/app.js` — PASS.
- `node frontend-web/scripts/knowledge-operations-console-smoke.mjs` — PASS.
- `node frontend-web/scripts/book-knowledge-web-smoke.mjs` — PASS.
- `npm install` from `frontend/` — PASS; npm reported existing audit
  findings: 1 low, 6 moderate, 9 high.
- `npm run build` from `frontend/` — PASS; Vite reported existing eval/chunk
  size warnings.
- Sandboxed `go test ./... -timeout=180s` failed because the sandbox denied
  local `httptest` port binding, DNS, and macOS keychain access.
- Non-sandbox rerun:
  `GOCACHE=/private/tmp/dedao-kbase-operations-diagnostics-deploy-go-cache DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-kbase-operations-diagnostics-deploy-config DEDAO_BOOK_KNOWLEDGE_ROOT=/private/tmp/dedao-kbase-operations-diagnostics-deploy-store go test ./... -timeout=180s`
  — PASS.
- `bash scripts/privacy-smoke.sh` — PASS.
- `git diff --check` — PASS.
- `git status --short` — clean before push.

### Server preflight

- Production host: `executor.life`.
- Service preflight:
  `systemctl is-active dedao-kbase` — `active`.
- Current health:
  `curl -fsS http://127.0.0.1:8719/health` —
  `{"ok":true,"service":"dedao-kbase"}`.
- Release archive:
  `/private/tmp/kbase-release-abc1d67.tar.gz`.
- Archive SHA-256:
  `43b64866fbf07265676d827b307054a0556e51ad9758ffccaf2380a050cc78c0`.
- Uploaded archive:
  `scp /private/tmp/kbase-release-abc1d67.tar.gz executor.life:/tmp/kbase-release-abc1d67.tar.gz`
  — PASS.
- Server-side package preflight:
  `node --check frontend-web/app.js`, all
  `frontend-web/scripts/*smoke*.mjs`,
  `/opt/go-toolchains/go1.23.0/bin/go test ./... -timeout=180s`,
  and
  `CGO_ENABLED=1 /opt/go-toolchains/go1.23.0/bin/go build -trimpath -o /tmp/kbase-server-abc1d67 ./cmd/kbase-server`
  — PASS.
- Built server binary SHA-256:
  `7c50b08dc3acb76d501225c058f1812b1a720cdf13dcac5a5a164b268ed2ebdd`.

### G5 · Deployment health

- Deployment command replaced `/opt/dedao-kbase/bin/kbase-server` and
  `/opt/dedao-kbase/frontend-web` with scoped backups and automatic rollback
  on restart or health failure.
- Installed binary SHA-256:
  `7c50b08dc3acb76d501225c058f1812b1a720cdf13dcac5a5a164b268ed2ebdd`.
- `systemctl is-active dedao-kbase` — `active`.
- `systemctl show dedao-kbase -p ExecMainStatus -p NRestarts --value` —
  `0`, `0`.
- `curl -fsS http://127.0.0.1:8719/health` —
  `{"ok":true,"service":"dedao-kbase"}`.
- Binary backup:
  `/opt/dedao-kbase/bin/kbase-server.backup-abc1d67-20260723153519`.
- Frontend backup:
  `/opt/dedao-kbase/frontend-web.backup-abc1d67-20260723153519`.

Decision: PASS.

### G6 · Online verification

- `curl -fsS https://kbase.executor.life/health` —
  `{"ok":true,"service":"dedao-kbase"}`.
- `curl -fsS https://health.executor.life/api/v1/health` —
  `{"status":"healthy","services":{"api":"running","database":"connected","redis":"connected","celery":"connected"}}`.
- Static asset checks on production local service:
  `/operations` contains `20260723-operations-diagnostics`;
  `/app.js` contains `health_review_diagnostics` and
  `Health Queue Diagnostics`;
  `/styles.css` contains `knowledge-operations__diagnostics`.
- Authenticated operations API verification:
  schema `knowledge_operations.v1`, total `5`, items `5`, queue `0`,
  diagnostic reason `no_items_match_current_limit`, safe actions `1`.
- Unsafe replay verification:
  `publish` with confirmation returned HTTP `409` and contained
  `not allowed`.
- Safe replay verification:
  `evaluate_quality` returned HTTP `200`, status `planned`, mutated `false`.
- Post-rollout service state:
  `systemctl is-active dedao-kbase` — `active`;
  `ExecMainStatus` — `0`;
  `NRestarts` — `0`.
- `journalctl -u dedao-kbase --since "5 minutes ago"` grep for
  `panic|fatal|error|failed` — no matches.

Decision: PASS.

## G4 · Safety Review Gate

- Diagnostics are read-only metadata derived from existing operations items and
  summary counts.
- No new route or mutating API was added.
- No downstream Health write, review decision, or serving promotion was added.
- Safe replay allowlist remains unchanged.
- Diagnostics action labels do not expose `publish` or
  `health_serving_promote`.
- No source bodies, downloaded content, prompts, tokens, cookies, or claim
  statements were added to docs, tests, or UI.

Decision: PASS.
