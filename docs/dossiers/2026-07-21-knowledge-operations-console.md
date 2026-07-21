# Knowledge Operations Console Dossier

**Status:** COMPLETE — G1-G6 PASS; production KBase deployed and verified

## S0 · User request

Approved. Continue with the Knowledge Operations Console direction and
first-release scope: Release Status Center, Health Evidence Review Workspace,
and Failure Explanation / Safe Replay.

Required boundaries:

- no automatic Health serving promotion;
- no source body exposure;
- no unsafe replay;
- explicit confirmation for dangerous actions;
- stop on failed Gate, genuine blocker, destructive action, or required secret
  or human decision.

## S1 · Discovery

- Repository instructions and `docs/system-map/INDEX.md` were read before
  implementation.
- Existing KBase surfaces already provide pipeline dashboard, immutable
  releases, quality reports, Health evidence readiness/evidence packages,
  feedback, and reverification retry.
- Existing frontend-web already has a knowledge workbench, pipeline panel,
  review panel, and agent package pages.
- Current feature branch was clean before this feature started.

## G1 · Admission

- first_class_objects: Knowledge Operations Console, operations item, Health
  review summary, failure explanation, safe replay request/result.
- core_loop_step: observe release state -> identify Health review blockers ->
  explain failure -> safely replay analysis or quality only.
- smallest_end_to_end_slice: one authenticated operator API and page showing
  one package's pipeline, release, Health readiness, failure explanation, and
  safe replay affordance.
- risk: medium-high because Health evidence and replay controls are involved.
- decision: PASS. Scope is visibility plus bounded replay and preserves
  consumer ownership.

## S2 · Product/design

Design document:
`docs/plans/2026-07-21-knowledge-operations-console-design.md`.

Decision: a KBase-owned operations aggregation API and frontend page, not a
Health serving or review mutation API.

## S3 · Plan

Implementation plan:
`docs/plans/2026-07-21-knowledge-operations-console.md`.

## G2 · Feasibility and safety

- Reuses existing KBase stores and functions.
- Operations API returns metadata and aggregate counts only; it does not include
  source bodies, prompts, tokens, or cookies.
- Replay allows only analysis and deterministic quality evaluation. Publish,
  Health serving promotion, feedback writes, and unknown actions fail closed.
- Health review ownership remains outside KBase; KBase reports readiness and
  evidence metadata only.
- decision: PASS.

## Checkpoints

### Task 1

- Created design, implementation plan, and dossier.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- Commit: `250d7e9 docs(kbase): design knowledge operations console`.

### Tasks 2-3

- RED: `go test ./backend/app -run TestBuildKnowledgeOperationsConsoleCombinesPipelineReleaseAndHealthState -count=1`
  failed with undefined `BuildKnowledgeOperationsConsole` and
  `KnowledgeOperationsSchemaVersion`.
- GREEN: `go test ./backend/app -run TestBuildKnowledgeOperationsConsoleCombinesPipelineReleaseAndHealthState -count=1`
  — PASS.
- RED: `go test ./backend/app -run TestKnowledgeOperationsHealthSummaryDoesNotExposeSourceBody -count=1`
  failed because Health claim/citation/risk counts were absent.
- GREEN:
  `go test ./backend/app -run 'Test(BuildKnowledgeOperationsConsoleCombinesPipelineReleaseAndHealthState|KnowledgeOperationsHealthSummaryDoesNotExposeSourceBody)' -count=1`
  — PASS.

### Task 4

- RED:
  `go test ./backend/app -run 'TestKnowledgeOperationsExplainsFailuresWithSafeReplay|TestRunKnowledgeOperationsReplayRejectsDangerousActions' -count=1`
  failed because `RunKnowledgeOperationsReplay` and request types were
  undefined.
- GREEN:
  `go test ./backend/app -run 'TestKnowledgeOperationsExplainsFailuresWithSafeReplay|TestRunKnowledgeOperationsReplayRejectsDangerousActions' -count=1`
  — PASS.
- Safe replay allows only `analyze` and `evaluate_quality`; `publish`,
  `health_serving_promote`, `feedback`, and unknown actions return a not-allowed
  error.

### Task 5

- RED: `go test ./backend/app -run 'TestKBaseHTTPHandlerKnowledgeOperations' -count=1`
  failed with missing routes (`404` for console, method routing for replay).
- GREEN:
  `go test ./backend/app -run 'TestKBaseHTTPHandlerKnowledgeOperations|Test(BuildKnowledgeOperationsConsole|KnowledgeOperationsHealthSummary|KnowledgeOperationsExplains|RunKnowledgeOperationsReplay)' -count=1`
  — PASS.
- Routes added after operator bearer authentication:
  `GET /api/knowledge/operations` and
  `POST /api/knowledge/operations/replay`.

### Task 6

- RED: `node frontend-web/scripts/knowledge-operations-console-smoke.mjs`
  failed because `knowledgeOperationsState` and route markers were absent.
- GREEN:
  `node --check frontend-web/app.js && node frontend-web/scripts/knowledge-operations-console-smoke.mjs`
  — PASS.
- Added `/operations` frontend-web route with Release Status Center, Health
  Evidence Review Workspace, Failure Explanation, and safe replay controls.

## G3 · Test Gate

- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json && bash scripts/system-map-smoke.sh`
  — PASS.
- `go test ./backend/app -run 'KnowledgeOperations' -count=1` — PASS.
- `go test ./... -timeout=180s` — PASS.
- `node --check frontend-web/app.js` — PASS.
- `for script in frontend-web/scripts/*smoke*.mjs; do node "$script"; done`
  — PASS, including the new Knowledge Operations smoke.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.

Decision: PASS.

## G4 · Safety Review Gate

- Operations aggregation returns release/pipeline/Health metadata and aggregate
  Health claim/citation/risk counts only.
- Regression coverage asserts claim statements are not serialized by
  `BuildKnowledgeOperationsConsole` or the HTTP console route.
- Replay runner rejects `publish`, `health_serving_promote`, `feedback`, and
  unknown actions even with `confirm=true`.
- Frontend smoke asserts unsafe replay labels are not rendered as buttons.
- Health serving remains owned by Health; KBase UI copy states it does not
  promote Health serving.
- No source body, prompt, token, cookie, or downloaded content was added to
  fixtures, docs, or UI.

Decision: PASS.

## G5/G6

## Push and main integration

- Feature implementation commit: `4360938 feat(kbase): add knowledge operations console`.
- `bash scripts/privacy-smoke.sh && git diff --check` before push — PASS.
- Pushed `dedao-kbase/codex/book-agent-platform` from `8190f33` to `4360938`.
- `bash scripts/privacy-smoke.sh && git diff --check` before main update —
  PASS.
- Fast-forwarded `dedao-kbase/main` from `8190f33` to `4360938`.

## G5 · Deployment Health Gate

- Clean main release clone at `43609385e64143def47cbf2f6f80badd815afcc4`.
- Initial clean clone `go test ./... -timeout=180s` failed before deployment
  because Wails `frontend/dist` was missing from the clean source tree:
  `main.go:18:12: pattern all:frontend/dist: no matching files found`.
  No deployment mutation had occurred. The release clone then ran
  `cd frontend && npm install && npm run build` to regenerate `frontend/dist`.
- Clean-main verification after generating `frontend/dist`:
  `go test ./... -timeout=180s` — PASS;
  `node --check frontend-web/app.js` — PASS;
  `for script in frontend-web/scripts/*smoke*.mjs; do node "$script"; done` —
  PASS;
  `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- Clean release archive with generated `frontend/dist` and without `.git` or
  `node_modules`: SHA-256
  `f31956cffcb9e074a7350be5b0dc71d2080ade23209f84f12beed260fcdbaf78`.
- Server-side preflight in `/tmp/kbase-release-4360938`:
  `node --check frontend-web/app.js` and all `frontend-web/scripts/*smoke*.mjs`
  — PASS; `/opt/go-toolchains/go1.23.0/bin/go test ./... -timeout=180s` —
  PASS.
- Server-side Linux build:
  `CGO_ENABLED=1 /opt/go-toolchains/go1.23.0/bin/go build -trimpath -o /tmp/kbase-server-4360938 ./cmd/kbase-server`
  — PASS; binary SHA-256
  `9a160285dcd1aad4a5e7dfe0cda5c44b4f6d35696c5bd2bcb7fb1aa9c205fcc8`.
- Deployment replaced only `/opt/dedao-kbase/bin/kbase-server` and
  `/opt/dedao-kbase/frontend-web`; KBase data/artifact directories were not
  touched.
- Backups:
  `/opt/dedao-kbase/bin/kbase-server.backup-4360938-20260721083423` and
  `/opt/dedao-kbase/frontend-web.backup-4360938-20260721083423`.
- Post-restart service checks:
  `systemctl is-active dedao-kbase` — `active`;
  `ExecMainStatus=0`;
  `NRestarts=0`;
  local `/health` returned `{"ok":true,"service":"dedao-kbase"}`.

Decision: PASS.

## G6 · Online Verification Gate

- Public `https://kbase.executor.life/health` returned
  `{"ok":true,"service":"dedao-kbase"}`.
- Public `https://health.executor.life/api/v1/health` returned healthy with
  API, database, Redis, and Celery connected.
- Deployed `/app.js` contains the new `Knowledge Operations Console`,
  `Release Status Center`, `Health Evidence Review Workspace`, and
  `/api/knowledge/operations` markers.
- Authenticated production `GET /api/knowledge/operations?limit=5` returned
  schema `knowledge_operations.v1`, `total=5`, `items=5`,
  `health_published=1`.
- Authenticated dangerous replay probe:
  `POST /api/knowledge/operations/replay` with action `publish` returned HTTP
  `409` and `replay action "publish" is not allowed`.
- Authenticated safe replay planning probe:
  `POST /api/knowledge/operations/replay` with action `evaluate_quality` and no
  confirmation returned `status=planned`, `mutated=false`.
- `systemctl is-active dedao-kbase` remained `active`; `ExecMainStatus=0`;
  `NRestarts=0`.
- `journalctl -u dedao-kbase --since "5 minutes ago"` contained no
  `panic|fatal|error|failed` lines.

Decision: PASS.

## Post-deployment usability hardening

- Follow-up public route check showed `https://kbase.executor.life/`,
  `/operations`, `/app.js`, and `/styles.css` return HTTP `401` without browser
  Basic Auth, while `/health` remains public. Server-local checks against
  `127.0.0.1:8719` returned `200` for the same static routes. Nginx config
  confirms `location /` intentionally uses `auth_basic "dedao-kbase"` and
  `/api/` remains bearer-protected. Decision: no anonymous static access change.
- Found a real browser-cache risk: `frontend-web/index.html` still referenced
  `/app.js?v=20260721-pipeline-timeout` and
  `/styles.css?v=20260721-pipeline-timeout`. TDD:
  changed `frontend-web/scripts/book-knowledge-web-smoke.mjs` to require
  `20260721-operations-console`; RED confirmed the old index failed; then
  updated `frontend-web/index.html` to the new cache version.
- Verification after cache-bust fix:
  `node frontend-web/scripts/book-knowledge-web-smoke.mjs &&
  node frontend-web/scripts/knowledge-operations-console-smoke.mjs &&
  node --check frontend-web/app.js` — PASS.
