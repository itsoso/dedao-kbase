# Book Knowledge Project Hub Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the book knowledge base into an organized project knowledge hub for health-llm-driven and Proofroom.

**Architecture:** Keep `dedao-gui` as the Dedao-facing knowledge asset service. Add a project layer over existing books, chunks, claims, and citations: project descriptors, review queues, and export previews. Downstream systems consume draft evidence packs and apply their own promotion gates.

**Tech Stack:** Go, JSON/JSONL book knowledge store, private kbase HTTP API, Vue 3/Vite web UI.

---

### Task 1: Project Knowledge Protocol

**Files:**
- Create: `backend/app/book_project.go`
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Steps:**
1. Add `BookKnowledgeProject`, `BookKnowledgeReviewQueueItem`, and `BookKnowledgeProjectExportPreview`.
2. Add health and proofroom project descriptors with explicit draft/review wording.
3. Build review queues from existing package claims, preserving `book_id`, `chapter_id`, `claim_id`, citations, confidence, and source status.
4. Add Bearer-protected endpoints:
   - `GET /api/projects`
   - `GET /api/projects/{project}/review-queue`
   - `GET /api/projects/{project}/export-preview`
5. Add tests that verify health/proofroom responses include draft review semantics and source IDs.

### Task 2: Web Project Hub Panel

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`
- Modify: `frontend-web/src/style.css`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Steps:**
1. Add TypeScript interfaces and client methods for project descriptors, queues, and previews.
2. Add a compact `项目知识` panel to the book knowledge workbench.
3. Show Health and Proofroom tabs, review counts, export preview counts, and the top review items.
4. Keep the UI read-only in this slice; no downstream import or claim promotion yet.
5. Add smoke assertions for the new panel and API methods.

### Task 3: Verification And Deploy

**Commands:**
- `go test ./backend/app -run 'TestKBaseHTTPHandlerServesProjectKnowledgeHub' -count=1`
- `node frontend-web/scripts/web-kbase-ui-smoke.mjs`
- `npm --prefix frontend-web run build`
- `go test ./...`
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server`
- `git diff --check`

**Deploy:** Install the rebuilt `kbase-server` binary to `/opt/dedao-kbase/bin/kbase-server`, sync `frontend-web/dist/` to the existing `kbase.executor.life` static directory, restart `dedao-kbase.service`, and verify `/health`, `/api/projects`, project preview endpoints, and deployed bundle markers.
