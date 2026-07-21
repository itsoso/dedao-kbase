# Knowledge Operations Console Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the first Knowledge Operations Console release: Release Status Center, Health Evidence Review Workspace, and Failure Explanation / Safe Replay.

**Architecture:** Add a privacy-safe operations aggregation service and authenticated HTTP routes that reuse existing KBase pipeline, release, Health evidence, analysis, and quality functions. Add a lightweight frontend-web route that displays the aggregate state and only exposes bounded replay actions.

**Tech Stack:** Go backend and tests, private KBase HTTP APIs, existing Health evidence contract, frontend-web vanilla JavaScript/CSS smoke tests, lifecycle dossier.

---

### Task 1: Lifecycle documents and Gate scaffold

**Files:**
- Create: `docs/plans/2026-07-21-knowledge-operations-console-design.md`
- Create: `docs/plans/2026-07-21-knowledge-operations-console.md`
- Create: `docs/dossiers/2026-07-21-knowledge-operations-console.md`

**Steps:**

1. Write the approved design with explicit scope, non-goals, API shape, safety boundaries, and tests.
2. Write this implementation plan with TDD tasks.
3. Create the dossier with S0-S3, G1, and G2.
4. Run `bash scripts/privacy-smoke.sh && git diff --check`.
5. Commit only the three lifecycle docs.

### Task 2: Release Status Center backend aggregation

**Files:**
- Create: `backend/app/knowledge_operations.go`
- Create: `backend/app/knowledge_operations_test.go`

**Steps:**

1. Write a failing test `TestBuildKnowledgeOperationsConsoleCombinesPipelineReleaseAndHealthState`.
2. Run `go test ./backend/app -run TestBuildKnowledgeOperationsConsoleCombinesPipelineReleaseAndHealthState -count=1`; expect RED because the builder does not exist.
3. Implement `BuildKnowledgeOperationsConsole(store, limit)` returning `knowledge_operations.v1`, summary counts, and items.
4. Include pipeline stage, latest release ID, quality decision, usage policy, and Health readiness status.
5. Run the focused test; expect GREEN.

### Task 3: Health Evidence Review Workspace summary

**Files:**
- Modify: `backend/app/knowledge_operations.go`
- Modify: `backend/app/knowledge_operations_test.go`

**Steps:**

1. Write a failing test `TestKnowledgeOperationsHealthSummaryDoesNotExposeSourceBody`.
2. Run the focused test; expect RED because Health review evidence metadata is missing.
3. Add Health summary fields: `serving_allowed=false`, reasons, claim count, citation count, risk counts, and freshness.
4. Ensure operations responses do not include source bodies or package chunk text.
5. Run the focused tests; expect GREEN.

### Task 4: Failure explanation and safe replay backend

**Files:**
- Modify: `backend/app/knowledge_operations.go`
- Modify: `backend/app/knowledge_operations_test.go`

**Steps:**

1. Write failing tests:
   - `TestKnowledgeOperationsExplainsFailuresWithSafeReplay`
   - `TestRunKnowledgeOperationsReplayRejectsDangerousActions`
2. Run the focused tests; expect RED.
3. Add deterministic failure explanation mapping for public error codes and stale/missing states.
4. Add `RunKnowledgeOperationsReplay(ctx, store, generator, request)` allowing only `analyze` and `evaluate_quality`.
5. Require `confirm=true` for mutating replay; `confirm=false` returns `planned`.
6. Reject `publish`, `health_serving_promote`, `feedback`, and unknown actions with errors.
7. Run the focused tests; expect GREEN.

### Task 5: Authenticated HTTP routes

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Steps:**

1. Write failing HTTP tests:
   - `TestKBaseHTTPHandlerKnowledgeOperationsConsole`
   - `TestKBaseHTTPHandlerKnowledgeOperationsReplayRejectsUnsafeActions`
2. Run `go test ./backend/app -run 'KnowledgeOperations' -count=1`; expect RED.
3. Add `GET /api/knowledge/operations`.
4. Add `POST /api/knowledge/operations/replay`.
5. Enforce auth, method, limit validation, and dangerous-action rejection.
6. Run the focused HTTP tests; expect GREEN.

### Task 6: Frontend-web operations page

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Create: `frontend-web/scripts/knowledge-operations-console-smoke.mjs`

**Steps:**

1. Write failing smoke script that checks route labels, status panels, Health review panel, failure explanation, and absence of unsafe replay labels.
2. Run `node frontend-web/scripts/knowledge-operations-console-smoke.mjs`; expect RED.
3. Add `/operations` route, navigation entry, state loader, render function, refresh button, and safe replay controls.
4. Add compact CSS.
5. Run smoke script; expect GREEN.

### Task 7: System-map, dossier, and full verification

**Files:**
- Modify if structural inventory changed: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-07-21-knowledge-operations-console.md`

**Steps:**

1. Regenerate system-map only if route/type inventory changed.
2. Run focused backend tests.
3. Run `go test ./... -timeout=180s`.
4. Run relevant frontend-web smoke scripts.
5. Run `bash scripts/privacy-smoke.sh && git diff --check`.
6. Update dossier with exact command results and G3/G4 decisions.
7. Commit only files from this feature.

### Task 8: Push and deployment gate

**Files:**
- Modify: `docs/dossiers/2026-07-21-knowledge-operations-console.md`

**Steps:**

1. Push the feature branch after G3/G4 pass.
2. Do not deploy unless G3 and G4 are PASS.
3. If deploying, use clean main only.
4. Perform online verification of the console API/page and confirm KBase and Health remain healthy.
5. Update dossier with G5/G6 evidence.
