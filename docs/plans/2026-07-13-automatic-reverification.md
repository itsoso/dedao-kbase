# Automatic Knowledge Reverification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn invalidating consumer feedback into durable asynchronous candidate re-analysis without automatic publication.

**Architecture:** Add file-backed reverification tasks to `BookKnowledgeStore`, enqueue them from the feedback handler, and process them with a single bounded background runner started by `kbase-server`. Reuse the current structured analysis and quality pipeline while preserving immutable releases and the explicit publish gate.

**Tech Stack:** Go, `net/http`, atomic JSON files, existing TokenPlan analysis client, Go tests.

---

### Task 1: Durable Queue Contract

**Files:**
- Create: `backend/app/knowledge_reverification.go`
- Create: `backend/app/knowledge_reverification_test.go`

1. Write failing tests for active-task coalescing, terminal-task replacement,
   cooldown deferral, due-task listing, and stale-running recovery.
2. Run `go test ./backend/app -run Reverification -count=1` and confirm failures
   are caused by the missing queue contract.
3. Implement the minimal file-backed task model and atomic store operations.
4. Rerun the focused tests and keep them green.

### Task 2: Candidate Runner

**Files:**
- Modify: `backend/app/knowledge_reverification.go`
- Modify: `backend/app/knowledge_reverification_test.go`

1. Write failing tests proving the runner marks a passing candidate ready,
   records model failures, detects changed content, and requeues when assessment
   advances during processing.
2. Run the focused tests and verify RED.
3. Implement a runner with injected analysis generator and clock.
4. Run focused tests and `go test ./backend/app -count=1`.

### Task 3: Feedback And Status API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

1. Write failing tests for automatic enqueue, duplicate POST coalescing,
   authenticated task listing, method rejection, and no synchronous generator
   invocation.
2. Run `go test ./backend/app -run 'KBaseHTTPHandler.*Reverification|KBaseHTTPHandlerKnowledgeFeedback' -count=1` and verify RED.
3. Add queue configuration to `KBaseHTTPConfig`, enqueue after durable feedback,
   and expose the nested status endpoint.
4. Rerun focused HTTP and package tests.

### Task 4: Server Lifecycle

**Files:**
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

1. Write failing tests for interval bounds and runner lifecycle.
2. Run `go test ./cmd/kbase-server -run Reverification -count=1` and verify RED.
3. Start the runner independently of source-agent authentication, with
   environment-bounded interval, cooldown, and stale-running timeout.
4. Rerun server tests.

### Task 5: Documentation And Full Gates

**Files:**
- Modify: `README.md`
- Modify: `docs/dossiers/2026-07-13-automatic-reverification.md`

1. Document the authenticated endpoint, environment controls, and explicit
   non-publication boundary.
2. Run `gofmt` on changed Go files.
3. Run `go test ./...`, `go vet ./...`, race tests for changed packages,
   `cd frontend && npm run build`, privacy smoke, and `git diff --check`.
4. Record G3/G4 evidence in the dossier and commit only task files.
5. Deploy from a clean merged custom main, verify public health, authenticated
   task status, asynchronous enqueue, and unchanged release availability.
