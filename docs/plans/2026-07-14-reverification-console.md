# Knowledge Reverification Console Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let operators inspect, retry, and explicitly publish feedback-driven knowledge candidates from the Web workbench.

**Architecture:** Extend the file-backed reverification store with one guarded manual retry operation and expose it through a nested authenticated endpoint. Compose existing release, feedback, quality, task, and publish APIs in the current two-column vanilla JavaScript workbench; no new frontend framework or automatic publication path is introduced.

**Tech Stack:** Go, `net/http`, atomic JSON files, vanilla JavaScript, CSS, Node smoke tests.

---

### Task 1: Guarded Retry Contract

**Files:**
- Modify: `backend/app/knowledge_reverification.go`
- Modify: `backend/app/knowledge_reverification_test.go`

1. Add failing tests for failed-task retry, active/ready rejection, and feedback
   fingerprint mismatch.
2. Run `go test ./backend/app -run ReverificationRetry -count=1` and verify RED.
3. Implement a lock-protected retry transition that resets attempts, due time,
   terminal fields, candidate fields, and the public error code.
4. Rerun focused tests and commit the store contract.

### Task 2: Retry HTTP Endpoint

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

1. Add failing tests for authenticated POST success, GET rejection, missing
   release, conflict mapping, and release listing filtered by `book_id` before
   pagination.
2. Run `go test ./backend/app -run KBaseHTTPHandlerKnowledgeReverificationRetry -count=1` and verify RED.
3. Route the nested retry resource, return the updated task, and apply the
   release `book_id` filter before cursor/limit pagination.
4. Rerun focused HTTP tests and commit.

### Task 3: Review State And Data Composition

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Extend the smoke script with required review state, endpoint, polling, reset,
   and confirmation markers; run it and verify RED.
2. Load release records, latest release detail, feedback assessment,
   reverification tasks, and quality report for the selected book.
3. Reset all review state on book changes and poll only queued/running tasks.
4. Add retry and explicit publish actions, then rerun the smoke test.

### Task 4: Compact Review Surface

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Add smoke markers for the collapsed status band, expanded evidence view,
   quality rules, candidate differences, and state-specific actions.
2. Render the console in the main knowledge column without changing the sidebar
   width or default reading flow.
3. Add restrained responsive styles and preserve the existing visual system.
4. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs`.

### Task 5: Full Gates And Rollout

**Files:**
- Modify: `README.md`
- Modify: `docs/dossiers/2026-07-14-reverification-console.md`

1. Document the retry endpoint and explicit-publication boundary.
2. Run `gofmt`, `go test ./...`, `go vet ./...`, race tests, both frontend
   builds, Web smoke tests, privacy smoke, and `git diff --check`.
3. Complete independent review and fix every Critical, High, or Medium finding.
4. Merge through PR, deploy from clean custom main, verify public health,
   authenticated read paths, and non-destructive console assets.
