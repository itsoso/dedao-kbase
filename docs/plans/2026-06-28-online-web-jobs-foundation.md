# Online Web Jobs Foundation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the first online-web execution slice: a protected Job API, persistent job records, and a Web Jobs panel for existing book export tasks.

**Architecture:** Add a JSON-backed job store under `BookKnowledgeStore` so it works in `CGO_ENABLED=0` deployments. Extend `kbase-server` HTTP routes with `/api/jobs` endpoints. Add frontend API types and a compact Jobs panel in the existing right rail.

**Tech Stack:** Go `net/http`, Go JSON file persistence, Vue 3, TypeScript, Vite, Node smoke script.

---

### Task 1: Backend Job API Test

**Files:**
- Modify: `backend/app/kbase_http_test.go`

**Steps:**
1. Add `TestKBaseHTTPHandlerServesJobs`.
2. Verify `GET /api/jobs` returns an empty list.
3. Verify `POST /api/jobs` with `{"type":"notebooklm_export","book_id":"42"}` returns a job id.
4. Poll `GET /api/jobs/{id}` until status is `succeeded`.
5. Verify the result contains NotebookLM export files.
6. Run `go test ./backend/app -run 'TestKBaseHTTPHandlerServesJobs' -count=1`.
7. Expected: FAIL with 404 before implementation.

### Task 2: Backend Job Store And Runner

**Files:**
- Create: `backend/app/book_jobs.go`
- Modify: `backend/app/kbase_http.go`

**Steps:**
1. Add job structs, request validation, JSON persistence, and status update helpers.
2. Add runner support for `notebooklm_export` and `book_export`.
3. Add `/api/jobs` and `/api/jobs/{id}` routes.
4. Run focused job test until it passes.
5. Run `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`.

### Task 3: Frontend Job Contract And API Client

**Files:**
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`
- Modify: `frontend-web/src/api.ts`

**Steps:**
1. Add smoke assertions for `jobs-panel`, `listJobs`, `createJob`, `getJob`, and `jobType`.
2. Run smoke and confirm it fails.
3. Add TypeScript job interfaces and KBaseClient methods.

### Task 4: Frontend Jobs Panel

**Files:**
- Modify: `frontend-web/src/App.vue`
- Modify: `frontend-web/src/style.css`

**Steps:**
1. Add `Jobs` tab to the right rail.
2. Add buttons for NotebookLM, Health KB, and Quant Rules jobs for the selected book.
3. Show recent jobs with status, timestamps, error, and result summary.
4. Refresh jobs after creating or reloading.

### Task 5: Verification And Deployment

**Commands:**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1
CGO_ENABLED=0 go test ./backend/app -run 'TestKBaseHTTPHandlerServesJobs' -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
git diff --check
```

Deploy updated binary and `frontend-web/dist`, then verify `/health`, `GET /api/jobs`, and a production `notebooklm_export` job.
