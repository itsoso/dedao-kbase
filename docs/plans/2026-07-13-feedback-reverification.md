# Feedback-Driven Reverification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Derive and expose a privacy-safe re-verification assessment from immutable knowledge release feedback.

**Architecture:** Add a pure assessment function over the existing JSONL feedback records, then expose that assessment through the existing authenticated release feedback route. Keep releases immutable and leave analysis and publication gates unchanged.

**Tech Stack:** Go, `BookKnowledgeStore`, `net/http`, JSONL persistence, existing smoke scripts.

---

### Task 1: Feedback assessment domain model

**Files:**
- Modify: `backend/app/knowledge_feedback.go`
- Modify: `backend/app/knowledge_feedback_test.go`

1. Write failing tests for healthy, zero-hit-only, stale, conflict, and rejected assessments.
2. Run `go test ./backend/app -run 'TestKnowledgeFeedbackAssessment' -count=1` and confirm RED.
3. Add the bounded assessment model and pure aggregation logic.
4. Re-run the focused tests and confirm GREEN.

### Task 2: Authenticated assessment API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

1. Write failing tests for feedback GET and POST assessment responses.
2. Run the focused HTTP tests and confirm RED.
3. Add GET support and include the assessment in POST responses.
4. Re-run focused tests and confirm GREEN.

### Task 3: Documentation and release gates

**Files:**
- Modify: `docs/dossiers/2026-07-13-feedback-reverification.md`

1. Run `gofmt` on changed Go files.
2. Run focused tests, `go test ./...`, frontend smoke checks, privacy smoke, and `git diff --check`.
3. Record Gate evidence and only deploy if every gate is green.
