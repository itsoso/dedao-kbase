# Knowledge Pipeline Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the A+B scope: a unified knowledge pipeline dashboard and a controlled one-shot automation action for newly collected content.

**Architecture:** Add a backend pipeline projection that derives each source/book item state from existing store artifacts: package, analysis manifest, quality report, and releases. Expose read and run-once HTTP endpoints, then surface them in the Web knowledge page as a compact dashboard with clear next actions.

**Tech Stack:** Go backend in `backend/app`, plain frontend in `frontend-web/app.js` and `frontend-web/styles.css`, smoke tests in `frontend-web/scripts`.

---

### Task 1: Backend Pipeline Projection

**Files:**
- Create: `backend/app/knowledge_pipeline_dashboard.go`
- Test: `backend/app/knowledge_pipeline_dashboard_test.go`

**Step 1: Write failing test**
Add a test that creates books with missing analysis, ready analysis, quality report, and release, then asserts stage labels and next actions.

**Step 2: Run test**
Run: `go test ./backend/app -run TestKnowledgePipelineDashboard`
Expected: fail because projection does not exist.

**Step 3: Implement projection**
Add `BuildKnowledgePipelineDashboard(store, limit)` returning summary counters and ordered items.

**Step 4: Verify**
Run: `go test ./backend/app -run TestKnowledgePipelineDashboard`
Expected: pass.

### Task 2: Automation Run Once

**Files:**
- Modify: `backend/app/knowledge_pipeline_dashboard.go`
- Test: `backend/app/knowledge_pipeline_dashboard_test.go`

**Step 1: Write failing test**
Add a test for `RunKnowledgePipelineAutomation` with a fake analysis generator. It should process only eligible pending items up to `limit`, evaluate quality, and report counts.

**Step 2: Run test**
Run: `go test ./backend/app -run TestRunKnowledgePipelineAutomation`
Expected: fail because runner does not exist.

**Step 3: Implement minimal runner**
Scan dashboard items. For `needs_analysis`, call the existing analysis generator. For ready analysis missing quality, call `EvaluateBookAnalysisQuality`. Do not publish automatically in this first pass.

**Step 4: Verify**
Run: `go test ./backend/app -run 'TestKnowledgePipelineDashboard|TestRunKnowledgePipelineAutomation'`
Expected: pass.

### Task 3: HTTP API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write failing test**
Assert `GET /api/knowledge/pipeline` returns items and summary, and `POST /api/knowledge/pipeline/run` accepts `{limit,dry_run}`.

**Step 2: Run test**
Run: `go test ./backend/app -run TestKBaseHTTPHandlerKnowledgePipeline`
Expected: fail with 404.

**Step 3: Implement endpoints**
Wire routes under existing authenticated API handling. Use `limit` query/body with small defaults.

**Step 4: Verify**
Run: `go test ./backend/app -run TestKBaseHTTPHandlerKnowledgePipeline`
Expected: pass.

### Task 4: Web Dashboard

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Write failing smoke assertions**
Assert the UI calls `/api/knowledge/pipeline`, `/api/knowledge/pipeline/run`, and renders “知识流水线” plus “自动推进一次”.

**Step 2: Run smoke**
Run: `node frontend-web/scripts/book-knowledge-web-smoke.mjs`
Expected: fail before UI exists.

**Step 3: Implement UI**
Add a dashboard section in `/book-knowledge` with stage counters, item list, next actions, dry-run preview, and one-shot run button.

**Step 4: Verify**
Run: `node --check frontend-web/app.js && node frontend-web/scripts/book-knowledge-web-smoke.mjs`
Expected: pass.

### Task 5: Release Checks

**Files:**
- Modify: `docs/_generated/system-map.json`

**Step 1: Regenerate system map**
Run: `go run ./cmd/system-map --root . --out docs/_generated/system-map.json && bash scripts/system-map-smoke.sh`

**Step 2: Full verification**
Run:
- `go test ./...`
- `cd frontend && npm run build`
- `bash scripts/privacy-smoke.sh`
- `git diff --check`

**Step 3: Commit, push, deploy**
Commit the scoped files, push to `main`, deploy, then verify `/health`, `/api/knowledge/pipeline`, and the Web resource version online.
