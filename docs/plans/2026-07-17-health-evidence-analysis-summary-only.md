# Health Evidence Analysis Summary Only Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a no-item summary mode for Health evidence analysis batches so dashboards and automation can poll queue health without model calls or large payloads.

**Architecture:** Extend `HealthEvidenceAnalysisBatchRequest` with `summary_only`. Reuse the existing batch summary computation, force summary-only requests to behave as dry-run previews, and skip candidate item generation.

**Tech Stack:** Go backend, existing KBase HTTP handler, existing Health evidence tests, generated system map, Markdown contract docs.

---

### Task 1: Add Summary-Only Batch Mode

**Files:**
- Modify: `backend/app/health_evidence.go`
- Modify: `backend/app/health_evidence_test.go`

**Step 1: Write the failing test**

Add a test that calls `RunHealthEvidenceAnalysisBatch` with `SummaryOnly: true` and a generator that fails if called. Assert it returns queue summary fields, `dry_run: true`, `processed: 0`, and no items.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatchSummaryOnly|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: FAIL because `SummaryOnly` does not exist yet.

**Step 3: Write minimal implementation**

Add `SummaryOnly bool` to the request. Compute summary first; if summary-only is set, return without processing candidates or calling the generator.

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatchSummaryOnly|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: PASS.

### Task 2: Document And Verify

**Files:**
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Document `summary_only`**

Explain that `summary_only` returns queue statistics only, does not include candidate items, and never triggers model calls.

**Step 2: Verify and deploy**

Run the standard smoke ladder, push to main, deploy, and verify production with a summary-only POST.
