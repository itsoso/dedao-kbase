# Health Evidence Analysis Dry Run Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a safe preview mode for Health readiness analysis so operators can see what would be analyzed without spending model calls or mutating knowledge artifacts.

**Architecture:** Reuse `RunHealthEvidenceAnalysisBatch` as the single execution seam and add a `dry_run` request flag. Dry runs select the same `needs_analysis` candidates as live runs, but skip model generation, manifest writes, and quality evaluation.

**Tech Stack:** Go backend, existing KBase HTTP handler, existing Health evidence tests, generated system map, Markdown contract docs.

---

### Task 1: Add Dry-Run Batch Behavior

**Files:**
- Modify: `backend/app/health_evidence.go`
- Modify: `backend/app/health_evidence_test.go`

**Step 1: Write the failing test**

Add a test that calls `RunHealthEvidenceAnalysisBatch` with `DryRun: true`, a generator that fails if called, and two `needs_analysis` books. Assert `processed`, `succeeded`, and returned item statuses are previews, and no analysis or quality report is written.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatchDryRun|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: FAIL because `DryRun` does not exist yet.

**Step 3: Write minimal implementation**

Add `DryRun bool` to `HealthEvidenceAnalysisBatchRequest`. In `RunHealthEvidenceAnalysisBatch`, when `DryRun` is true, append a preview item and continue without calling the generator.

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatchDryRun|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: PASS.

### Task 2: Cover HTTP And Docs

**Files:**
- Modify: `backend/app/health_evidence_test.go`
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Extend HTTP test**

POST `{"limit":1,"dry_run":true}` to `/api/consumers/health/readiness/analyze`; assert `preview` status and no generator call.

**Step 2: Update contract docs**

Document `dry_run` as a no-mutation, no-model-call preview mode.

**Step 3: Verify**

Run narrow tests, regenerate the system map, then run the standard smoke ladder.
