# Health Evidence Analysis Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Health evidence analysis batch responses explain candidate counts, skipped readiness states, and limit truncation.

**Architecture:** Extend `HealthEvidenceAnalysisBatchResult` without adding routes. `RunHealthEvidenceAnalysisBatch` keeps using the readiness report as its source of truth, counts every inspected item by readiness status, processes only `needs_analysis`, and reports whether the request limit stopped processing.

**Tech Stack:** Go backend, existing KBase HTTP handler, existing Health evidence tests, generated system map, Markdown contract docs.

---

### Task 1: Add Batch Summary Fields

**Files:**
- Modify: `backend/app/health_evidence.go`
- Modify: `backend/app/health_evidence_test.go`

**Step 1: Write the failing test**

Add assertions that dry-run and live batch results include `dry_run`, `eligible`, `skipped`, `skipped_by_status`, and `limit_reached`.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatch|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: FAIL because the new fields do not exist yet.

**Step 3: Write minimal implementation**

Add summary fields to `HealthEvidenceAnalysisBatchResult`. Count every readiness item before processing, increment `eligible` for `needs_analysis`, count other states in `skipped_by_status`, and set `limit_reached` when there are more eligible items after the request limit.

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatch|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: PASS.

### Task 2: Update Contract And Verification

**Files:**
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Document fields**

Document the batch summary fields and their meaning.

**Step 2: Verify**

Run full backend/frontend/smoke/privacy checks, then deploy and verify a production dry-run response includes the summary.
