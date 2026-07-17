# Health Evidence Analysis Remaining After Batch Plan

**Goal:** Help external schedulers decide whether to keep looping after the next Health evidence analysis batch.

**Architecture:** Keep the existing `/api/consumers/health/readiness/analyze` endpoint. Add `remaining_after_next_batch` to `HealthEvidenceAnalysisBatchResult`, computed as `eligible - next_batch_size` after request limit normalization. The value is available for live, dry-run, and summary-only responses.

**Implementation Tasks**

1. Add tests for limited, full, summary-only, and complete queues.
2. Populate `remaining_after_next_batch` during batch summary calculation.
3. Update the knowledge supply contract and generated system map.
4. Run the standard smoke ladder, push to `main`, deploy, and verify production returns the field.

**Expected Result:** Automation can run one batch, inspect whether work remains, and stop without recomputing queue math client-side.
