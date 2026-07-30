# KBase Baseline Reconciliation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Produce a clean, verified KBase branch from the latest product
baseline while retaining the only confirmed security behavior missing from the
legacy working-tree patch.

**Architecture:** Treat `dedao-kbase/main` as the sole baseline. Compare changes
at the behavior level, reject superseded or incidental file changes, and add
the missing HTTP error-boundary protection directly to the current handler with
a focused regression test.

**Tech Stack:** Go 1.21, `net/http`, repository Go tests, Vue 3/Vite,
Node-based Web smoke scripts, shell release and privacy gates.

---

### Task 1: Establish and verify the clean baseline

**Files:**
- Inspect: `docs/system-map/INDEX.md`
- Inspect: `docs/_generated/system-map.json`
- Inspect: `docs/dossiers/2026-07-11-kbase-wechat-collector.md`

**Step 1: Confirm branch and working-tree state**

Run: `git status --short --branch`

Expected: clean `codex/baseline-reconciliation` branch based on
`dedao-kbase/main`.

**Step 2: Run baseline backend tests**

Run: `go test ./... -count=1`

Expected: PASS before feature changes.

**Step 3: Run baseline frontend build**

Run: `cd frontend && npm run build`

Expected: type-check and Vite build PASS.

### Task 2: Classify the legacy patch

**Files:**
- Compare: `backend/app/kbase_http.go`
- Compare: `backend/app/kbase_http_test.go`
- Compare: `frontend-web/app.js`
- Compare: `frontend-web/styles.css`
- Compare: `frontend/package-lock.json`
- Compare: `go.mod`
- Compare: `build/`

**Step 1: Confirm superseded behavior**

Search the current baseline for the legacy API routes, Web workflows, cache
handling, and first-party source-agent capability.

Expected: current implementations are present and newer.

**Step 2: Reject unsafe or incidental changes**

Do not migrate tracked build-asset deletions, the Go 1.25 upgrade, indirect
dependency churn, or the old Web bundle wholesale.

**Step 3: Isolate missing behavior**

Trace the `POST /api/book-chat` missing-book error from storage through the HTTP
handler.

Expected: the raw storage error can cross the HTTP boundary.

### Task 3: Add the missing-book chat regression test

**Files:**
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write a focused failing test**

Add a test that posts to `/api/book-chat` for a missing book and asserts:

- HTTP status is `404`;
- the response contains `book not found`;
- the response does not contain the temporary root, `manifest.json`, or the
  knowledge-store directory name.

**Step 2: Verify RED**

Run:
`go test ./backend/app -run TestKBaseHTTPHandlerBookChatMissingBookDoesNotExposeFilesystemPath -count=1`

Expected: FAIL because the handler returns an internal error containing a local
path.

### Task 4: Sanitize the missing-book chat response

**Files:**
- Modify: `backend/app/kbase_http.go`

**Step 1: Implement the smallest fix**

In `handleBookChat`, detect a missing package with `os.IsNotExist`, sanitize the
requested book ID using existing helpers, and return
`404 book not found: <id>`.

**Step 2: Verify GREEN**

Run:
`go test ./backend/app -run TestKBaseHTTPHandlerBookChatMissingBookDoesNotExposeFilesystemPath -count=1`

Expected: PASS.

**Step 3: Verify related handler behavior**

Run:
`go test ./backend/app -run 'TestKBaseHTTPHandler(BookChat|MissingBook)' -count=1`

Expected: PASS.

### Task 5: Run repository verification gates

**Files:**
- Verify: `backend/app/kbase_http.go`
- Verify: `backend/app/kbase_http_test.go`
- Verify: `docs/plans/2026-07-30-baseline-reconciliation-design.md`
- Verify: `docs/plans/2026-07-30-baseline-reconciliation.md`

**Step 1: Run full backend verification**

Run: `go test ./... -count=1`

Expected: PASS.

**Step 2: Run frontend verification**

Run: `cd frontend && npm run build`

Expected: PASS.

**Step 3: Run repository smoke and drift gates**

Run the system-map smoke, applicable Web smoke scripts, packaging smoke, and
release-gate scripts discovered in the repository.

Expected: every local gate PASS; no test output is piped through plain `tail`.

**Step 4: Run privacy and diff checks**

Run:

- `bash scripts/privacy-smoke.sh`
- `git diff --check`

Expected: PASS.

**Step 5: Commit only reconciled files**

Stage the two HTTP files and the two plan files explicitly, then commit with a
short scoped subject.

Expected: the legacy working tree remains untouched and no dependency,
generated output, or build-asset change is included.

