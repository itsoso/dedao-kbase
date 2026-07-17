# Health Evidence Analysis Queue State Implementation Plan

> **For implementers:** Execute this plan task-by-task and keep verification output with the change.

**Goal:** Add explicit queue-state decision fields to Health evidence analysis responses so dashboards and automation can decide whether to run analysis, review blockers, or stay idle.

**Architecture:** Extend `HealthEvidenceAnalysisBatchResult` with `scanned`, `has_work`, `queue_state`, and `recommended_action`. Compute these from the existing readiness summary before any item processing, so live, dry-run, and summary-only responses share the same decision semantics.

**Tech Stack:** Go backend, existing KBase HTTP handler, existing Health evidence tests, generated system map, Markdown contract docs.

---

### Task 1: Add Queue-State Fields

**Files:**
- Modify: `backend/app/health_evidence.go`
- Modify: `backend/app/health_evidence_test.go`

**Step 1: Write the failing test**

Assert that batch responses include `scanned`, `has_work`, `queue_state`, and `recommended_action`. Use existing mixed-status fixtures so the expected state is `ready` and action is `run_analysis`.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatch|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: FAIL because the new fields do not exist.

**Step 3: Write minimal implementation**

Compute:
- `scanned`: number of readiness items inspected
- `has_work`: `eligible > 0`
- `queue_state`: `ready`, `blocked`, or `empty`
- `recommended_action`: `run_analysis`, `review_blocked`, or `idle`

**Step 4: Run test to verify it passes**

Run: `go test ./backend/app -run 'HealthEvidenceAnalysisBatch|RunHealthEvidenceAnalysisBatch' -count=1`

Expected: PASS.

### Task 2: Document And Verify

**Files:**
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Regenerate: `docs/_generated/system-map.json`

**Step 1: Document fields**

Document the queue-state fields and recommended action vocabulary.

**Step 2: Verify and deploy**

Run the standard smoke ladder, push to main, deploy, and verify production summary-only response includes the new fields.
