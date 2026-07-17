# Health Evidence Analysis Batch Estimates Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add lightweight batch-size estimates to Health evidence analysis responses so dashboards can calculate remaining work without duplicating server rules.

**Architecture:** Extend `HealthEvidenceAnalysisBatchResult` with normalized request limit, next batch size, and estimated batch count. Compute these from the existing `eligible` count after request limit normalization; this affects dry-run, summary-only, and live responses equally.

**Tech Stack:** Go backend, existing KBase HTTP handler, existing Health evidence tests, generated system map, Markdown contract docs.

---

### Task 1: Add Estimate Fields

**Files:**
- Modify: `backend/app/health_evidence.go`
- Modify: `backend/app/health_evidence_test.go`

**Step 1: Write the failing test**

Add assertions that batch responses include `requested_limit`, `next_batch_size`, and `estimated_batches` for live, dry-run, and summary-only requests.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatch|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: FAIL because the new fields do not exist yet.

**Step 3: Write minimal implementation**

Add the fields to `HealthEvidenceAnalysisBatchResult` and compute:
- `requested_limit`: normalized request limit
- `next_batch_size`: `min(eligible, requested_limit)`
- `estimated_batches`: ceiling division of eligible by requested limit

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatch|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: PASS.

### Task 2: Document And Verify

**Files:**
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Document fields**

Document the estimate fields and that they are safe for polling.

**Step 2: Verify and deploy**

Run the standard smoke ladder, push to main, deploy, and verify production summary-only response includes the new fields.
