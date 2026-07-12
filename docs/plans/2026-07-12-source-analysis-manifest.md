# Source Analysis Manifest Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Give every imported source article a durable, traceable analysis manifest that can be generated with TokenPlan, inspected in the Web UI, and consumed through REST.

**Architecture:** Source ingestion creates a `pending` manifest beside the article knowledge package without calling an external model inside the ingestion transaction. A dedicated analysis service runs grounded TokenPlan analysis on demand, persists the result atomically, and exposes GET/POST REST endpoints. The article detail page reads the manifest and offers generation or regeneration while keeping free-form article chat available.

**Tech Stack:** Go, JSON files under the existing `BookKnowledgeStore`, TokenPlan OpenAI-compatible chat, `net/http`, vanilla `frontend-web` JavaScript/CSS, Go unit tests and frontend smoke tests.

---

### Task 1: Add Durable Analysis Manifest Storage

**Files:**
- Create: `backend/app/book_analysis.go`
- Create: `backend/app/book_analysis_test.go`
- Modify: `backend/app/source_ingest.go`
- Modify: `backend/app/source_ingest_test.go`

**Steps:**
1. Write failing tests for atomic save/load, missing-manifest behavior, and source ingestion creating a `pending` manifest tied to the package content hash.
2. Run `go test ./backend/app -run 'Test(BookAnalysisManifest|IngestSourceArticleCreatesPendingAnalysis)' -count=1` and confirm the tests fail because the storage API does not exist.
3. Add the manifest schema and `BookKnowledgeStore` methods for `analysis_manifest.json`.
4. Initialize or invalidate the manifest after a new or updated source package is saved; do not modify it for skipped content.
5. Run the focused tests and `go test ./backend/app -run 'TestIngestSourceArticle' -count=1`.

### Task 2: Generate Grounded Analysis and Persist It

**Files:**
- Modify: `backend/app/book_analysis.go`
- Modify: `backend/app/book_analysis_test.go`

**Steps:**
1. Write a failing test using a fake LLM client that verifies the prompt requests summary, claims, risks, actions, and source IDs, and verifies the completed manifest is persisted.
2. Run `go test ./backend/app -run 'TestGenerateBookAnalysisManifest' -count=1` and confirm RED.
3. Implement generation using the existing grounded book context and TokenPlan configuration. Persist `running`, then `ready` or `failed`, retaining the previous successful output until replacement succeeds.
4. Run the focused tests.

### Task 3: Expose Manifest REST Endpoints

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Steps:**
1. Write failing tests for `GET /api/books/{book_id}/analysis` and `POST /api/books/{book_id}/analysis`, including 404, invalid JSON, and generated response cases.
2. Run `go test ./backend/app -run 'TestKBaseHTTPHandlerBookAnalysis' -count=1` and confirm RED.
3. Route the nested resource before the generic book handler and enforce GET/POST method handling.
4. Run the focused tests and `go test ./backend/app -count=1`.

### Task 4: Show Analysis State and Results in the Article UI

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Steps:**
1. Extend the smoke test with failing assertions for the manifest endpoint, lifecycle labels, and generate/regenerate action.
2. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs` and confirm RED.
3. Load the manifest with the selected article, render status, generated answer, provenance, and error state, and POST generation with the selected model.
4. Keep free-form prompts separate from the durable baseline analysis and reset both when switching articles.
5. Run the smoke test and `node --check frontend-web/app.js`.

### Task 5: Release Verification

**Files:**
- Modify: `docs/plans/2026-07-12-source-analysis-manifest.md`

**Steps:**
1. Run `go test ./...`.
2. Run the book-knowledge and source-control frontend smoke tests.
3. Run `bash scripts/privacy-smoke.sh` and `git diff --check`.
4. Record the exact verification evidence in this plan before commit or deployment.

## Verification Record

Status: implemented and verified locally on 2026-07-12.

- `go test ./backend/app -count=1`: passed.
- `go test ./...`: passed.
- `node frontend-web/scripts/book-knowledge-web-smoke.mjs`: passed.
- `node frontend-web/scripts/wechat-collector-control-plane-smoke.mjs`: passed.
- `node frontend-web/scripts/wcplus-control-plane-smoke.mjs`: passed.
- `node frontend-web/scripts/kbase-token-header-smoke.mjs`: passed.
- `node --check frontend-web/app.js`: passed.
- Headless Chrome interaction at 1440x1000 and 390x844: passed generation, rendered Markdown, and minimum-width checks.
- `bash scripts/privacy-smoke.sh`: passed.
- `git diff --check`: passed.
